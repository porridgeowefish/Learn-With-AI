package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

type GitHub struct {
	db                   *store.Store
	id, secret, callback string
	client               *http.Client
}

func NewGitHub(db *store.Store, id, secret, callback string) (*GitHub, error) {
	endpoint, err := url.Parse(callback)
	if db == nil || id == "" || secret == "" || err != nil || endpoint.Scheme != "https" ||
		endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "/v1/auth/github/callback" ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, ErrInvalid
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return &GitHub{db: db, id: id, secret: secret, callback: callback, client: client}, nil
}

type GitHubStart struct {
	FlowID           string `json:"flowId"`
	AuthorizationURL string `json:"authorizationUrl"`
}

func (g *GitHub) Start(ctx context.Context, challenge, owner, ownerToken string) (GitHubStart, error) {
	proof, err := base64.RawURLEncoding.DecodeString(challenge)
	if err != nil || len(proof) != sha256.Size || (owner == "") != (ownerToken == "") {
		return GitHubStart{}, ErrInvalid
	}
	state := rand.Text()
	var bytes [32]byte
	_, _ = rand.Read(bytes[:])
	verifier := base64.RawURLEncoding.EncodeToString(bytes[:])
	digest := sha256.Sum256([]byte(verifier))
	flowID := rand.Text()
	var ownerSession []byte
	if ownerToken != "" {
		ownerSession = sessionDigest(ownerToken)
	}
	if err := g.db.StartGitHub(ctx, store.GitHubFlow{
		ID: flowID, Owner: owner, Verifier: verifier, StateDigest: sessionDigest(state), DesktopChallenge: proof,
		OwnerSession: ownerSession,
		Expires:      time.Now().Add(10 * time.Minute),
	}); err != nil {
		return GitHubStart{}, err
	}
	parameters := url.Values{
		"client_id": {g.id}, "redirect_uri": {g.callback}, "scope": {"user:email"}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"},
	}
	return GitHubStart{FlowID: flowID, AuthorizationURL: "https://github.com/login/oauth/authorize?" + parameters.Encode()}, nil
}

func (g *GitHub) Callback(ctx context.Context, state, code string) error {
	if state == "" || len(state) > 100 || len(code) > 1024 {
		return ErrInvalid
	}
	flow, err := g.db.ClaimGitHubCallback(ctx, sessionDigest(state), time.Now())
	if err != nil {
		return err
	}
	reason := "failed"
	if code != "" {
		err = g.complete(ctx, flow, code)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, store.ErrConflict):
			reason = "email_conflict"
		case errors.Is(err, store.ErrCode):
			reason = "email_required"
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return errors.Join(store.ErrGitHub, g.db.FailGitHub(cleanup, flow.ID, reason))
}

func (g *GitHub) complete(ctx context.Context, flow store.GitHubFlow, code string) error {
	form := url.Values{
		"client_id": {g.id}, "client_secret": {g.secret}, "redirect_uri": {g.callback},
		"code": {code}, "code_verifier": {flow.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		Access string `json:"access_token"`
	}
	if err := g.decode(req, &token); err != nil || token.Access == "" {
		return store.ErrGitHub
	}
	var identity struct {
		ID int64 `json:"id"`
	}
	if err := g.user(ctx, "/user", token.Access, &identity); err != nil || identity.ID <= 0 {
		return store.ErrGitHub
	}
	var emails []struct {
		Email             string
		Verified, Primary bool
	}
	var address string
	if g.user(ctx, "/user/emails?per_page=100", token.Access, &emails) == nil {
		for _, value := range emails {
			if value.Verified && value.Primary && !strings.HasSuffix(value.Email, "@users.noreply.github.com") {
				address, _ = normalizeEmail(value.Email)
				break
			}
		}
	}
	return g.db.CompleteGitHub(ctx, flow.ID, strconv.FormatInt(identity.ID, 10), address, time.Now())
}

func (g *GitHub) user(ctx context.Context, path, token string, body any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	return g.decode(req, body)
}

func (g *GitHub) decode(req *http.Request, body any) error {
	response, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return store.ErrGitHub
	}
	return json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(body)
}

func (g *GitHub) Finish(ctx context.Context, flowID, verifier, ownerToken string) (Session, error) {
	if len(flowID) > 100 || len(verifier) < 43 || len(verifier) > 128 {
		return Session{}, ErrInvalid
	}
	now := time.Now()
	token := rand.Text()
	expires := now.Add(sessionTTL)
	var ownerSession []byte
	if ownerToken != "" {
		ownerSession = sessionDigest(ownerToken)
		token = ownerToken
	}
	account, expires, err := g.db.RedeemGitHub(ctx, flowID, sessionDigest(verifier), ownerSession, sessionDigest(token), now, expires)
	if err != nil {
		return Session{}, err
	}
	return Session{Account: account, Token: token, ExpiresAt: expires}, nil
}

func (s *Service) SendGitHubConfirmation(ctx context.Context, flowID, verifier string) error {
	if len(flowID) > 100 || len(verifier) < 43 || len(verifier) > 128 {
		return ErrInvalid
	}
	email, err := s.store.GitHubConfirmationEmail(ctx, flowID, sessionDigest(verifier), s.now())
	if err != nil {
		return err
	}
	return s.sendScopedCode(ctx, email, store.GitHubEmailPurpose, store.GitHubEmailPurpose+":"+flowID)
}

func (s *Service) ConfirmGitHubEmail(ctx context.Context, flowID, verifier, code string) error {
	if len(flowID) > 100 || len(verifier) < 43 || len(verifier) > 128 || !validCode(code) {
		return ErrInvalid
	}
	now := s.now()
	proof := sessionDigest(verifier)
	email, err := s.store.GitHubConfirmationEmail(ctx, flowID, proof, now)
	if err != nil {
		return err
	}
	digest := s.codeDigest(email, store.GitHubEmailPurpose+":"+flowID, code)
	return s.store.ConfirmGitHubEmail(ctx, flowID, proof, email, digest, now)
}
