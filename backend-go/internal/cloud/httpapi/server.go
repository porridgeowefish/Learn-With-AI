// Package httpapi exposes account and inference services over the cloud HTTP API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/auth"
	"github.com/xmz14/lll/backend-go/internal/cloud/inference"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
)

type Streamer interface {
	Stream(context.Context, string, int, string, ai.GatewayRequest, func(ai.GatewayEvent)) ([]inference.Attempt, error)
}

type Server struct {
	budget         Budget
	trustedProxies []netip.Prefix
	db             *store.Store
	accounts       *auth.Service
	gateway        Streamer
	models         map[string]Model
	list           []Model
	guestModels    []GuestModel
	prompts        atomic.Pointer[promptState]
	comparisonKey  []byte
	promptPublish  sync.Mutex
	mux            *http.ServeMux
	limit          authLimit
	modelMu        sync.RWMutex
	routeRevision  string
	routeAffinity  routeAffinity
	adminAuthorize func(http.ResponseWriter, *http.Request, bool) (store.Account, bool)
}

func New(db *store.Store, accounts *auth.Service, gateway Streamer, models []Model) (*Server, error) {
	if db == nil || accounts == nil || gateway == nil {
		return nil, errors.New("cloud API services are required")
	}
	s := &Server{
		db: db, accounts: accounts, gateway: gateway, mux: http.NewServeMux(),
		models: make(map[string]Model), list: append([]Model{}, models...),
	}
	var err error
	if s.models, err = modelIndex(models); err != nil {
		return nil, err
	}
	s.guestModels = guestModelCatalog(models)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	s.mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		s.modelMu.RLock()
		list := s.list
		s.modelMu.RUnlock()
		respond(w, http.StatusOK, list)
	})
	s.mux.HandleFunc("GET /v1/models/guest", func(w http.ResponseWriter, _ *http.Request) {
		s.modelMu.RLock()
		list := s.guestModels
		s.modelMu.RUnlock()
		respond(w, http.StatusOK, list)
	})
	s.mux.HandleFunc("POST /v1/auth/email/send", s.sendCode)
	s.mux.HandleFunc("POST /v1/auth/email/verify", s.verifyEmail)
	s.mux.HandleFunc("POST /v1/auth/email/bind/send", s.sendBindingEmail)
	s.mux.HandleFunc("POST /v1/auth/email/bind/verify", s.bindEmail)
	s.mux.HandleFunc("POST /v1/auth/{provider}/unbind", s.unlinkIdentity)
	s.mux.HandleFunc("POST /v1/auth/password", s.loginPassword)
	s.mux.HandleFunc("POST /v1/auth/refresh", s.refresh)
	s.mux.HandleFunc("POST /v1/auth/logout", s.logout)
	s.mux.HandleFunc("GET /v1/account", s.account)
	s.mux.HandleFunc("POST /v1/account/credentials", s.setLoginCredentials)
	s.mux.HandleFunc("POST /v1/credits/daily", s.claimDaily)
	s.mux.HandleFunc("GET /v1/credits/daily/history", s.dailyClaimHistory)
	s.mux.HandleFunc("POST /v1/credits/settlements", s.creditSettlements)
	s.mux.HandleFunc("POST /v1/inference", s.stream)
	s.mux.HandleFunc("POST /v1/reply-feedback", s.submitReplyFeedback)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	s.mux.ServeHTTP(w, r)
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]string{"error": code})
}

func decode(w http.ResponseWriter, r *http.Request, dst any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		fail(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func bearer(r *http.Request) string {
	value, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found || len(value) > 128 {
		return ""
	}
	return value
}

func (s *Server) authenticated(w http.ResponseWriter, r *http.Request) (store.Account, bool) {
	token := bearer(r)
	if token == "" {
		fail(w, http.StatusUnauthorized, "login_required")
		return store.Account{}, false
	}
	account, err := s.accounts.Account(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusUnauthorized, "login_required")
		return store.Account{}, false
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "account_unavailable")
		return store.Account{}, false
	}
	return account, true
}

func (s *Server) sendCode(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Purpose string }
	if !decode(w, r, &in, 8192) || !s.allowAuth(w, r, "send", in.Email, 10) {
		return
	}
	err := s.accounts.SendCode(r.Context(), in.Email, in.Purpose)
	if err != nil {
		authError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var in auth.EmailInput
	if !decode(w, r, &in, 8192) || !s.allowAuth(w, r, "verify", in.Email, 20) {
		return
	}
	session, err := s.accounts.VerifyEmail(r.Context(), in)
	writeSession(w, session, err)
}

func (s *Server) sendBindingEmail(w http.ResponseWriter, r *http.Request) {
	account, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	var in struct{ Email string }
	if !decode(w, r, &in, 8192) ||
		!s.allowAuth(w, r, "bind-send", account.ID, 5) ||
		!s.allowAuth(w, r, "send", in.Email, 10) {
		return
	}
	if err := s.accounts.SendBindingEmail(r.Context(), bearer(r), in.Email); err != nil {
		authError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) bindEmail(w http.ResponseWriter, r *http.Request) {
	account, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	var in struct{ Email, Code, Password string }
	if !decode(w, r, &in, 8192) || !s.allowAuth(w, r, "bind-verify", account.ID, 5) {
		return
	}
	linked, err := s.accounts.BindEmail(r.Context(), bearer(r), in.Email, in.Code, in.Password)
	if err != nil {
		authError(w, err)
		return
	}
	respond(w, http.StatusOK, linked)
}

func (s *Server) unlinkIdentity(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if provider != "email" && provider != "github" {
		http.NotFound(w, r)
		return
	}
	account, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	var in struct{ Password string }
	if !decode(w, r, &in, 8192) || !s.allowAuth(w, r, "unbind", account.ID, 5) {
		return
	}
	updated, err := s.accounts.UnlinkIdentity(r.Context(), bearer(r), provider, in.Password)
	if err != nil {
		authError(w, err)
		return
	}
	respond(w, http.StatusOK, updated)
}

func (s *Server) loginPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if !decode(w, r, &in, 8192) || !s.allowAuth(w, r, "password", in.Username, 10) {
		return
	}
	session, err := s.accounts.LoginPassword(r.Context(), in.Username, in.Password)
	writeSession(w, session, err)
}

func (s *Server) setLoginCredentials(w http.ResponseWriter, r *http.Request) {
	current, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	var in struct{ Username, Password, ConfirmPassword string }
	if !decode(w, r, &in, 8192) || !s.allowAuth(w, r, "set-credentials", current.ID, 5) {
		return
	}
	account, err := s.accounts.SetLoginCredentials(r.Context(), bearer(r), in.Username, in.Password, in.ConfirmPassword)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusUnauthorized, "login_required")
		return
	}
	if err != nil {
		authError(w, err)
		return
	}
	respond(w, http.StatusOK, account)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	session, err := s.accounts.Refresh(r.Context(), bearer(r))
	writeSession(w, session, err)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.accounts.Logout(r.Context(), bearer(r)); err != nil {
		authError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeSession(w http.ResponseWriter, session auth.Session, err error) {
	if err != nil {
		authError(w, err)
		return
	}
	respond(w, http.StatusOK, struct {
		Token     string        `json:"token"`
		ExpiresAt time.Time     `json:"expiresAt"`
		Account   store.Account `json:"account"`
	}{session.Token, session.ExpiresAt, session.Account})
}

func authError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalid):
		fail(w, http.StatusBadRequest, "invalid_account_input")
	case errors.Is(err, auth.ErrCredentials), errors.Is(err, store.ErrCode), errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusUnauthorized, "invalid_credentials")
	case errors.Is(err, store.ErrConflict):
		fail(w, http.StatusConflict, "account_conflict")
	case errors.Is(err, store.ErrCooldown):
		w.Header().Set("Retry-After", "60")
		fail(w, http.StatusTooManyRequests, "code_cooldown")
	case errors.Is(err, auth.ErrDelivery):
		fail(w, http.StatusServiceUnavailable, "email_unavailable")
	default:
		fail(w, http.StatusInternalServerError, "account_unavailable")
	}
}

func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	wallet, err := s.db.Wallet(r.Context(), user.ID)
	if err != nil {
		authError(w, err)
		return
	}
	claimed, err := s.db.DailyClaimed(r.Context(), user.ID, time.Now())
	if err != nil {
		authError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"account": user, "wallet": wallet, "dailyClaimed": claimed})
}

func (s *Server) claimDaily(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	wallet, err := s.db.ClaimDaily(r.Context(), user.ID, time.Now())
	if err != nil {
		authError(w, err)
		return
	}
	respond(w, http.StatusOK, wallet)
}

func (s *Server) dailyClaimHistory(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	history, err := s.db.DailyClaimHistory(r.Context(), user.ID, time.Now())
	if err != nil {
		authError(w, err)
		return
	}
	respond(w, http.StatusOK, history)
}

func (s *Server) creditSettlements(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	var in struct{ RequestIDs []string }
	if !decode(w, r, &in, 32<<10) {
		return
	}
	if len(in.RequestIDs) == 0 || len(in.RequestIDs) > 128 {
		fail(w, http.StatusBadRequest, "invalid_credit_requests")
		return
	}
	for _, id := range in.RequestIDs {
		if !identifier.MatchString(id) {
			fail(w, http.StatusBadRequest, "invalid_credit_requests")
			return
		}
	}
	settled, err := s.db.SettledCredits(r.Context(), user.ID, in.RequestIDs)
	if err != nil {
		fail(w, http.StatusInternalServerError, "credits_unavailable")
		return
	}
	respond(w, http.StatusOK, settled)
}

type authLimit struct {
	mu     sync.Mutex
	minute int64
	hits   map[string]int
}

func (s *Server) allowAuth(w http.ResponseWriter, r *http.Request, operation, subject string, count int) bool {
	peer := s.clientIP(r)
	subject = strings.ToLower(strings.TrimSpace(subject))
	now := time.Now().Unix() / 60
	s.limit.mu.Lock()
	defer s.limit.mu.Unlock()
	if s.limit.hits == nil || s.limit.minute != now {
		s.limit.minute = now
		s.limit.hits = make(map[string]int)
	}
	peerKey, subjectKey := operation+":ip:"+peer, operation+":subject:"+subject
	if len(s.limit.hits) >= 20_000 || s.limit.hits[peerKey] >= count || s.limit.hits[subjectKey] >= count {
		w.Header().Set("Retry-After", "60")
		fail(w, http.StatusTooManyRequests, "authentication_rate_limited")
		return false
	}
	s.limit.hits[peerKey]++
	s.limit.hits[subjectKey]++
	return true
}

// TrustProxies enables X-Real-IP only for explicitly configured reverse proxies.
func (s *Server) TrustProxies(cidrs []string) error {
	proxies := make([]netip.Prefix, 0, len(cidrs))
	for _, value := range cidrs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() == 0 {
			return errors.New("invalid trusted proxy CIDR")
		}
		proxies = append(proxies, prefix)
	}
	s.trustedProxies = proxies
	return nil
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	for _, proxy := range s.trustedProxies {
		if proxy.Contains(peer) {
			if forwarded, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil && forwarded.Zone() == "" {
				return forwarded.Unmap().String()
			}
			break
		}
	}
	return peer.String()
}
