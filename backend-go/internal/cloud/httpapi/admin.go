package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/promptcatalog"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

type Operator struct {
	Role    string
	TOTPKey []byte
}

// EnableAdmin installs operator-only operations before serving traffic.
func (s *Server) EnableAdmin(operators map[string]Operator) error {
	config := make(map[string]Operator, len(operators))
	for principal, operator := range operators {
		principal = strings.ToLower(strings.TrimSpace(principal))
		if principal == "" || (len(operator.TOTPKey) > 0 && len(operator.TOTPKey) < 20) || (operator.Role != "admin" && operator.Role != "operator") {
			return errors.New("invalid operator configuration")
		}
		if id, byAccount := strings.CutPrefix(principal, "account:"); byAccount {
			decoded, err := hex.DecodeString(id)
			if err != nil || len(decoded) != 16 {
				return errors.New("invalid operator account ID")
			}
		}
		if _, exists := config[principal]; exists {
			return errors.New("duplicate operator configuration")
		}
		operator.TOTPKey = append([]byte(nil), operator.TOTPKey...)
		config[principal] = operator
	}
	lookup := func(user store.Account) (Operator, bool) {
		if operator, ok := config["account:"+user.ID]; ok {
			return operator, true
		}
		operator, ok := config[user.Email]
		return operator, ok
	}
	issueSession := func(w http.ResponseWriter, r *http.Request, user store.Account, operator Operator, baseToken string, counter int64, now time.Time) {
		token := rand.Text()
		digest := sha256.Sum256([]byte(token))
		baseDigest := sha256.Sum256([]byte(baseToken))
		expires := now.Add(15 * time.Minute)
		if err := s.db.PutAdminSession(r.Context(), user.ID, counter, baseDigest[:], digest[:], now, expires); err != nil {
			authError(w, err)
			return
		}
		respond(w, http.StatusOK, map[string]any{"token": token, "expiresAt": expires, "role": operator.Role})
	}
	s.mux.HandleFunc("POST /v1/admin/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Username, Password string }
		if !decode(w, r, &in, 8192) || !s.allowAuth(w, r, "admin-password", in.Username, 10) {
			return
		}
		session, err := s.accounts.LoginPassword(r.Context(), in.Username, in.Password)
		if err != nil {
			authError(w, err)
			return
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
			defer cancel()
			_ = s.accounts.Logout(ctx, session.Token)
		}()
		operator, found := lookup(session.Account)
		if !found || len(operator.TOTPKey) != 0 {
			fail(w, http.StatusForbidden, "operator_required")
			return
		}
		issueSession(w, r, session.Account, operator, session.Token, 0, time.Now())
	})
	s.mux.HandleFunc("POST /v1/admin/session", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authenticated(w, r)
		if !ok {
			return
		}
		operator, found := lookup(user)
		if !found || len(operator.TOTPKey) == 0 {
			fail(w, http.StatusForbidden, "operator_required")
			return
		}
		var in struct{ Code string }
		if !decode(w, r, &in, 4096) || !s.allowAuth(w, r, "admin", user.ID, 5) {
			return
		}
		now := time.Now()
		counter, valid := verifyTOTP(operator.TOTPKey, in.Code, now)
		if !valid {
			fail(w, http.StatusUnauthorized, "invalid_operator_code")
			return
		}
		issueSession(w, r, user, operator, bearer(r), counter, now)
	})
	allow := func(w http.ResponseWriter, r *http.Request, write bool) (store.Account, bool) {
		token := bearer(r)
		digest := sha256.Sum256([]byte(token))
		user, err := s.db.AdminAccount(r.Context(), digest[:], time.Now())
		operator, exists := lookup(user)
		if err != nil || !exists || (write && operator.Role != "admin") {
			fail(w, http.StatusForbidden, "operator_required")
			return store.Account{}, false
		}
		return user, true
	}
	s.adminAuthorize = allow
	s.mux.HandleFunc("POST /v1/admin/logout", func(w http.ResponseWriter, r *http.Request) {
		if token := bearer(r); token != "" {
			digest := sha256.Sum256([]byte(token))
			if err := s.db.RevokeAdminSession(r.Context(), digest[:]); err != nil {
				fail(w, http.StatusInternalServerError, "operator_unavailable")
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s.mux.HandleFunc("GET /v1/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		days, err := strconv.Atoi(r.URL.Query().Get("days"))
		if err != nil {
			days = 30
		}
		stats, err := s.db.Metrics(r.Context(), time.Now(), days)
		if err != nil {
			fail(w, http.StatusInternalServerError, "metrics_unavailable")
			return
		}
		respond(w, http.StatusOK, stats)
	})
	s.mux.HandleFunc("POST /v1/admin/credits/adjust", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := allow(w, r, true)
		if !ok {
			return
		}
		var in struct {
			AccountID, Reference, Reason string
			Amount                       int64
		}
		if !decode(w, r, &in, 8192) {
			return
		}
		if !identifier.MatchString(in.AccountID) || !identifier.MatchString(in.Reference) ||
			strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 512 || in.Amount == 0 {
			fail(w, http.StatusBadRequest, "invalid_adjustment")
			return
		}
		wallet, err := s.db.AdjustCredits(r.Context(), admin.ID, in.AccountID, in.Reference, in.Reason, in.Amount, time.Now())
		switch {
		case errors.Is(err, store.ErrBalance), errors.Is(err, store.ErrConflict):
			fail(w, http.StatusConflict, "credit_adjustment_conflict")
		case errors.Is(err, store.ErrNotFound):
			fail(w, http.StatusNotFound, "account_not_found")
		case err != nil:
			fail(w, http.StatusInternalServerError, "credits_unavailable")
		default:
			respond(w, http.StatusOK, wallet)
		}
	})
	s.mux.HandleFunc("POST /v1/admin/accounts/list", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, true); !ok {
			return
		}
		var in struct {
			Keyword string
			Offset  int
		}
		if !decode(w, r, &in, 4096) {
			return
		}
		in.Keyword = strings.TrimSpace(in.Keyword)
		if len(in.Keyword) > 254 || in.Offset < 0 || in.Offset > 1_000_000 {
			fail(w, http.StatusBadRequest, "invalid_account_search")
			return
		}
		page, err := s.db.ListAccounts(r.Context(), in.Keyword, in.Offset)
		if err != nil {
			fail(w, http.StatusInternalServerError, "account_search_unavailable")
			return
		}
		respond(w, http.StatusOK, page)
	})
	s.mux.HandleFunc("POST /v1/admin/accounts/daily-credit-limit", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := allow(w, r, true)
		if !ok {
			return
		}
		var in struct {
			AccountID, Reference, Reason string
			Units                        *int64
		}
		if !decode(w, r, &in, 8192) {
			return
		}
		in.Reason = strings.TrimSpace(in.Reason)
		if !identifier.MatchString(in.AccountID) || !identifier.MatchString(in.Reference) || in.Units == nil ||
			*in.Units < 0 || *in.Units > store.MaxDailyCreditLimit || in.Reason == "" || len(in.Reason) > 512 {
			fail(w, http.StatusBadRequest, "invalid_daily_credit_limit")
			return
		}
		switch err := s.db.SetDailyCreditLimit(r.Context(), admin.ID, in.AccountID, in.Reference, in.Reason, *in.Units, time.Now()); {
		case errors.Is(err, store.ErrNotFound):
			fail(w, http.StatusNotFound, "account_not_found")
		case errors.Is(err, store.ErrConflict):
			fail(w, http.StatusConflict, "daily_credit_limit_conflict")
		case err != nil:
			fail(w, http.StatusInternalServerError, "credits_unavailable")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	s.mux.HandleFunc("POST /v1/admin/accounts/search", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, true); !ok {
			return
		}
		var in struct{ Keyword string }
		if !decode(w, r, &in, 4096) {
			return
		}
		keyword := strings.TrimSpace(in.Keyword)
		if len(keyword) < 2 || len(keyword) > 254 {
			fail(w, http.StatusBadRequest, "invalid_account_search")
			return
		}
		result, err := s.db.SearchAccounts(r.Context(), keyword)
		if err != nil {
			fail(w, http.StatusInternalServerError, "account_search_unavailable")
			return
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/admin/experiments", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		result, err := s.db.ExperimentMetrics(r.Context(), time.Now().AddDate(0, 0, -90))
		if err != nil {
			fail(w, http.StatusInternalServerError, "metrics_unavailable")
			return
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/admin/experiments/usage", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		days, err := strconv.Atoi(r.URL.Query().Get("days"))
		if err != nil {
			days = 30
		}
		result, err := s.db.ExperimentDailyUsage(r.Context(), time.Now(), days)
		if err != nil {
			fail(w, http.StatusInternalServerError, "metrics_unavailable")
			return
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/admin/experiments/cohorts", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		type entry struct {
			promptcatalog.ExperimentAllocation
			store.ExperimentCohort
			AllocationCheck allocationDiagnostic `json:"allocationCheck"`
		}
		result := []entry{}
		if state := s.prompts.Load(); state != nil {
			allocations := state.active.ExperimentAllocations()
			if len(allocations) > 0 {
				ids := make([]string, 0, len(allocations))
				for _, item := range allocations {
					ids = append(ids, item.ID)
				}
				counts, err := s.db.ExperimentCohorts(r.Context(), time.Now().AddDate(0, 0, -30), ids)
				if err != nil {
					fail(w, http.StatusInternalServerError, "metrics_unavailable")
					return
				}
				for _, item := range allocations {
					cohort := counts[item.ID]
					result = append(result, entry{item, cohort, checkExperimentAllocation(item.TreatmentPercent, cohort)})
				}
			}
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/admin/reconciliation", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, true); !ok {
			return
		}
		result, err := s.db.PendingInference(r.Context(), time.Now())
		if err != nil {
			fail(w, http.StatusInternalServerError, "reconciliation_unavailable")
			return
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/admin/reconciliation/history", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, true); !ok {
			return
		}
		query := r.URL.Query()
		account, request := query.Get("accountId"), query.Get("requestId")
		offset, err := strconv.Atoi(query.Get("offset"))
		if (query.Has("offset") && err != nil) || offset < 0 || offset > 1_000_000 ||
			(account != "" && !identifier.MatchString(account)) || (request != "" && !identifier.MatchString(request)) {
			fail(w, http.StatusBadRequest, "invalid_reconciliation_search")
			return
		}
		result, err := s.db.ReconciliationHistory(r.Context(), account, request, offset)
		if err != nil {
			fail(w, http.StatusInternalServerError, "reconciliation_unavailable")
			return
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("POST /v1/admin/reconciliation", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := allow(w, r, true)
		if !ok {
			return
		}
		var in struct {
			AccountID, RequestID, Reason string
			Units, CostNanos             *int64
		}
		if !decode(w, r, &in, 8192) {
			return
		}
		if !identifier.MatchString(in.AccountID) || !identifier.MatchString(in.RequestID) ||
			in.Units == nil || in.CostNanos == nil || *in.Units < 0 || *in.CostNanos < 0 ||
			strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 512 {
			fail(w, http.StatusBadRequest, "invalid_reconciliation")
			return
		}
		err := s.db.ReconcileInference(r.Context(), actor.ID, in.AccountID, in.RequestID, in.Reason, *in.Units, *in.CostNanos, time.Now())
		switch {
		case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrBalance):
			fail(w, http.StatusConflict, "reconciliation_conflict")
		case err != nil:
			fail(w, http.StatusInternalServerError, "reconciliation_unavailable")
		default:
			respond(w, http.StatusOK, map[string]bool{"ok": true})
		}
	})
	return nil
}

func verifyTOTP(secret []byte, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	counter := now.Unix() / 30
	for _, step := range []int64{counter, counter - 1, counter + 1} {
		if hmac.Equal([]byte(totpCode(secret, step)), []byte(code)) {
			return step, true
		}
	}
	return 0, false
}

func totpCode(secret []byte, step int64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(step))
	digest := hmac.New(sha1.New, secret)
	digest.Write(buf[:])
	sum := digest.Sum(nil)
	offset := sum[len(sum)-1] & 15
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000)
}
