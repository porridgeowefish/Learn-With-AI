package llmcloud

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GoogleFlow struct {
	State, AuthorizationURL string
	redirect, verifier      string
	sessionVersion          uint64
}

func (c *Client) GoogleAvailable(ctx context.Context) bool {
	_, err := c.googleClientID(ctx)
	return err == nil
}

func (c *Client) googleClientID(ctx context.Context) (string, error) {
	var result struct{ ClientID string }
	_, err := c.json(ctx, http.MethodGet, "v1/auth/google/config", "", nil, &result)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(result.ClientID, ".apps.googleusercontent.com") {
		return "", errors.New("invalid Google OAuth client")
	}
	return result.ClientID, nil
}

func (c *Client) StartGoogle(ctx context.Context, redirect string) (GoogleFlow, error) {
	c.mu.RLock()
	version := c.sessionVersion
	c.mu.RUnlock()
	id, err := c.googleClientID(ctx)
	if err != nil {
		return GoogleFlow{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(randomGoogleVerifier())
	challenge := sha256.Sum256([]byte(verifier))
	state := rand.Text()
	parameters := url.Values{
		"client_id": {id}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {"openid email"},
		"state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
	}
	return GoogleFlow{State: state, AuthorizationURL: "https://accounts.google.com/o/oauth2/v2/auth?" + parameters.Encode(),
		redirect: redirect, verifier: verifier, sessionVersion: version}, nil
}

func randomGoogleVerifier() []byte {
	var secret [32]byte
	_, _ = rand.Read(secret[:])
	return secret[:]
}

func (c *Client) FinishGoogle(ctx context.Context, flow GoogleFlow, code string) (Account, error) {
	c.mu.RLock()
	version := c.sessionVersion
	c.mu.RUnlock()
	if flow.sessionVersion != version {
		return Account{}, ErrLoginRequired
	}
	var next session
	input := map[string]string{"code": code, "verifier": flow.verifier, "redirectURI": flow.redirect}
	if _, err := c.json(ctx, http.MethodPost, "v1/auth/google/finish", "", input, &next); err != nil {
		return Account{}, err
	}
	if next.Token == "" || next.Account.ID == "" || !time.Now().Before(next.ExpiresAt) {
		return Account{}, errors.New("invalid cloud session response")
	}
	c.mu.Lock()
	if flow.sessionVersion != c.sessionVersion {
		c.mu.Unlock()
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_, _ = c.json(cleanup, http.MethodPost, "v1/auth/logout", next.Token, nil, nil)
		return Account{}, ErrLoginRequired
	}
	c.setSessionLocked(next)
	c.mu.Unlock()
	return next.Account, nil
}
