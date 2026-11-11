package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

var (
	ErrGoogle      = errors.New("google authorization failed")
	ErrGoogleEmail = errors.New("google did not verify an authoritative email")
)

type Google struct {
	id, secret string
	db         *store.Store
	client     *http.Client
}

func NewGoogle(db *store.Store, id, secret string) (*Google, error) {
	if db == nil || !strings.HasSuffix(id, ".apps.googleusercontent.com") || secret == "" {
		return nil, errors.New("invalid Google desktop OAuth configuration")
	}
	return &Google{id: id, secret: secret, db: db, client: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (g *Google) ClientID() string { return g.id }

func validGoogleRedirect(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.Path != "/api/cloud/google/callback" || u.RawPath != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
		return false
	}
	host, port, err := net.SplitHostPort(u.Host)
	number, portErr := strconv.ParseUint(port, 10, 16)
	return err == nil && host == "127.0.0.1" && portErr == nil && number > 0
}

func (g *Google) Finish(ctx context.Context, code, verifier, redirect string) (Session, error) {
	proof, err := base64.RawURLEncoding.DecodeString(verifier)
	if err != nil || len(proof) != 32 || len(code) == 0 || len(code) > 4096 || !validGoogleRedirect(redirect) {
		return Session{}, ErrInvalid
	}
	form := url.Values{
		"client_id": {g.id}, "client_secret": {g.secret}, "redirect_uri": {redirect},
		"code": {code}, "code_verifier": {verifier}, "grant_type": {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Session{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		Access string `json:"access_token"`
	}
	if err := g.decode(req, &token); err != nil || token.Access == "" {
		return Session{}, ErrGoogle
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return Session{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.Access)
	var identity struct {
		Subject  string `json:"sub"`
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Domain   string `json:"hd"`
	}
	if err := g.decode(req, &identity); err != nil || identity.Subject == "" || len(identity.Subject) > 255 {
		return Session{}, ErrGoogle
	}
	email, err := normalizeEmail(identity.Email)
	_, domain, _ := strings.Cut(email, "@")
	if err != nil || !identity.Verified || (domain != "gmail.com" && domain != "googlemail.com" && !strings.EqualFold(domain, identity.Domain)) {
		return Session{}, ErrGoogleEmail
	}
	now := time.Now()
	expires := now.Add(sessionTTL)
	value := rand.Text()
	account, err := g.db.SignInGoogle(ctx, identity.Subject, email, sessionDigest(value), now, expires)
	if err != nil {
		return Session{}, err
	}
	return Session{Account: account, Token: value, ExpiresAt: expires}, nil
}

func (g *Google) decode(req *http.Request, dst any) error {
	response, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrGoogle
	}
	return json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(dst)
}
