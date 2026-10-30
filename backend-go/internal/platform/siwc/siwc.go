// Package siwc owns the local Sign in with ChatGPT OAuth session. Tokens never
// enter the browser-facing model configuration or a third-party API endpoint.
package siwc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const resource = "https://api.openai.com/v1"
const authBase = "https://auth.openai.com"
const scopes = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"

var mu sync.Mutex
var pending *attempt
var lastError string
var client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type record struct {
	HostID       string    `json:"hostId"`
	ClientID     string    `json:"clientId,omitempty"`
	Subject      string    `json:"subject,omitempty"`
	Email        string    `json:"email,omitempty"`
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	IDToken      string    `json:"idToken,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}
type attempt struct {
	state, nonce, verifier, redirectURI, clientID string
	listener                                      net.Listener
}
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
}
type StatusInfo struct {
	Connected bool   `json:"connected"`
	Email     string `json:"email,omitempty"`
	Error     string `json:"error,omitempty"`
	Pending   bool   `json:"pending"`
}

func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Learn-With-AI", "siwc.json"), nil
}
func read() (record, error) {
	name, err := path()
	if err != nil {
		return record{}, err
	}
	b, err := os.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return record{}, nil
	}
	if err != nil {
		return record{}, err
	}
	var r record
	err = json.Unmarshal(b, &r)
	return r, err
}
func write(r record) error {
	name, err := path()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), "siwc-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}
func random() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b), err
}

func Status() (StatusInfo, error) {
	mu.Lock()
	defer mu.Unlock()
	r, err := read()
	if err != nil {
		return StatusInfo{}, err
	}
	return StatusInfo{Connected: r.RefreshToken != "" && strings.Contains(" "+r.Scope+" ", " chatgpt.tokens.use.direct "), Email: r.Email, Error: lastError, Pending: pending != nil}, nil
}

// Start opens a one-at-a-time loopback callback and returns the URL for the
// system browser. The URL has no credential; it is never logged by this package.
func Start() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if pending != nil {
		return "", errors.New("sign-in already in progress")
	}
	r, err := read()
	if err != nil {
		return "", err
	}
	if r.HostID == "" {
		id, e := random()
		if e != nil {
			return "", e
		}
		r.HostID = "urn:uuid:" + uuidFromRandom(id)
		if err = write(r); err != nil {
			return "", err
		}
	}
	state, err := random()
	if err != nil {
		return "", err
	}
	nonce, err := random()
	if err != nil {
		return "", err
	}
	verifier, err := random()
	if err != nil {
		return "", err
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	a := &attempt{state: state, nonce: nonce, verifier: verifier, redirectURI: "http://" + l.Addr().String() + "/auth/callback", clientID: r.ClientID, listener: l}
	pending = a
	lastError = ""
	q := url.Values{"response_type": {"code"}, "client_id": {r.ClientID}, "redirect_uri": {a.redirectURI}, "scope": {scopes}, "resource": {resource}, "state": {state}, "nonce": {nonce}, "code_challenge_method": {"S256"}, "code_challenge": {challenge(verifier)}, "ext_agent_host_id": {r.HostID}}
	if r.ClientID == "" {
		q.Set("client_id", "dynamic_agent_client")
		q.Set("agent_name_hint", "Learn-With-AI")
	}
	if r.Email != "" {
		q.Set("login_hint", r.Email)
	}
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/auth/callback" {
			http.NotFound(w, req)
			return
		}
		if req.URL.Query().Get("state") != a.state {
			http.Error(w, "Invalid sign-in state", 400)
			return
		}
		if req.URL.Query().Get("error") != "" {
			finish(a, "authorization declined")
			fmt.Fprint(w, "Authorization was not completed. You may close this tab.")
			go srv.Close()
			return
		}
		code := req.URL.Query().Get("code")
		issued := req.URL.Query().Get("client_id")
		if a.clientID != "" {
			if issued != "" && issued != a.clientID {
				http.Error(w, "Client mismatch", 400)
				return
			}
			issued = a.clientID
		}
		if code == "" || issued == "" {
			http.Error(w, "Incomplete sign-in callback", 400)
			return
		}
		if err := complete(req.Context(), a, issued, code); err != nil {
			finish(a, err.Error())
			http.Error(w, "Sign-in failed. Return to LLL and retry.", 400)
		} else {
			finish(a, "")
			fmt.Fprint(w, "ChatGPT is connected to Learn-With-AI. You may close this tab.")
		}
		go srv.Close()
	})
	go func() { _ = srv.Serve(l) }()
	go func() {
		time.Sleep(5 * time.Minute)
		mu.Lock()
		active := pending == a
		mu.Unlock()
		if active {
			finish(a, "sign-in timed out")
			_ = srv.Close()
		}
	}()
	return authBase + "/api/accounts/authorize?" + q.Encode(), nil
}
func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func uuidFromRandom(encoded string) string {
	b, _ := base64.RawURLEncoding.DecodeString(encoded)
	b = b[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
func finish(a *attempt, message string) {
	mu.Lock()
	defer mu.Unlock()
	if pending == a {
		pending = nil
		lastError = message
	}
}
func exchange(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", authBase+"/api/accounts/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return tokenResponse{}, fmt.Errorf("OpenAI token exchange returned %d", res.StatusCode)
	}
	var t tokenResponse
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&t)
	return t, err
}
func complete(ctx context.Context, a *attempt, issued, code string) error {
	t, err := exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "client_id": {issued}, "code": {code}, "code_verifier": {a.verifier}, "redirect_uri": {a.redirectURI}, "resource": {resource}})
	if err != nil {
		return err
	}
	if t.AccessToken == "" || t.RefreshToken == "" || !strings.Contains(" "+t.Scope+" ", " chatgpt.tokens.use.direct ") {
		return errors.New("ChatGPT plan permission was not granted")
	}
	identity, err := verifyIDToken(ctx, t.IDToken, issued, a.nonce)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	r, err := read()
	if err != nil {
		return err
	}
	if r.Subject != "" && r.Subject != identity.Subject {
		return errors.New("different ChatGPT account selected")
	}
	r.ClientID = issued
	r.Subject = identity.Subject
	r.Email = identity.Email
	r.AccessToken = t.AccessToken
	r.RefreshToken = t.RefreshToken
	r.IDToken = t.IDToken
	r.Scope = t.Scope
	r.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	return write(r)
}

type identity struct{ Subject, Email string }

func verifyIDToken(ctx context.Context, raw, clientID, nonce string) (identity, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return identity{}, errors.New("invalid ID token")
	}
	headerBytes, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return identity{}, e
	}
	var header struct{ Alg, Kid string }
	if e = json.Unmarshal(headerBytes, &header); e != nil {
		return identity{}, e
	}
	if header.Alg != "RS256" || header.Kid == "" {
		return identity{}, errors.New("unexpected ID token algorithm")
	}
	docReq, e := http.NewRequestWithContext(ctx, "GET", authBase+"/.well-known/openid-configuration", nil)
	if e != nil {
		return identity{}, e
	}
	docRes, e := client.Do(docReq)
	if e != nil {
		return identity{}, e
	}
	defer docRes.Body.Close()
	if docRes.StatusCode != 200 {
		return identity{}, errors.New("OIDC discovery failed")
	}
	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if e = json.NewDecoder(io.LimitReader(docRes.Body, 1<<20)).Decode(&doc); e != nil {
		return identity{}, e
	}
	u, e := url.Parse(doc.JWKSURI)
	if e != nil || u.Scheme != "https" || u.Hostname() != "auth.openai.com" {
		return identity{}, errors.New("untrusted JWKS URL")
	}
	keyReq, e := http.NewRequestWithContext(ctx, "GET", doc.JWKSURI, nil)
	if e != nil {
		return identity{}, e
	}
	keyRes, e := client.Do(keyReq)
	if e != nil {
		return identity{}, e
	}
	defer keyRes.Body.Close()
	if keyRes.StatusCode != 200 {
		return identity{}, errors.New("JWKS unavailable")
	}
	var keys struct {
		Keys []struct{ Kid, Kty, Alg, N, E string } `json:"keys"`
	}
	if e = json.NewDecoder(io.LimitReader(keyRes.Body, 1<<20)).Decode(&keys); e != nil {
		return identity{}, e
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[2])
	if e != nil {
		return identity{}, e
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	verified := false
	for _, k := range keys.Keys {
		if k.Kid != header.Kid || k.Kty != "RSA" {
			continue
		}
		n, ne := base64.RawURLEncoding.DecodeString(k.N)
		exp, ee := base64.RawURLEncoding.DecodeString(k.E)
		if ne != nil || ee != nil || len(exp) > 8 {
			continue
		}
		var exponent uint64
		for _, x := range exp {
			exponent = exponent<<8 | uint64(x)
		}
		if exponent < 3 || exponent > 1<<31 {
			continue
		}
		pub := rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exponent)}
		if rsa.VerifyPKCS1v15(&pub, crypto.SHA256, digest[:], sig) == nil {
			verified = true
			break
		}
	}
	if !verified {
		return identity{}, errors.New("ID token signature rejected")
	}
	claimsBytes, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return identity{}, e
	}
	var claims struct {
		Iss, Sub, Email, Nonce string
		Aud                    json.RawMessage
		Exp                    int64
	}
	if e = json.Unmarshal(claimsBytes, &claims); e != nil {
		return identity{}, e
	}
	if claims.Iss != authBase || claims.Sub == "" || claims.Nonce != nonce || claims.Exp <= time.Now().Unix() {
		return identity{}, errors.New("ID token claims rejected")
	}
	var audience string
	_ = json.Unmarshal(claims.Aud, &audience)
	if audience != clientID {
		var audiences []string
		_ = json.Unmarshal(claims.Aud, &audiences)
		found := false
		for _, v := range audiences {
			if v == clientID {
				found = true
			}
		}
		if !found {
			return identity{}, errors.New("ID token audience rejected")
		}
	}
	return identity{claims.Sub, claims.Email}, nil
}

func AccessToken(ctx context.Context) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	r, err := read()
	if err != nil {
		return "", err
	}
	if r.RefreshToken == "" || !strings.Contains(" "+r.Scope+" ", " chatgpt.tokens.use.direct ") {
		return "", errors.New("ChatGPT is not connected")
	}
	if r.AccessToken != "" && time.Until(r.ExpiresAt) > 2*time.Minute {
		return r.AccessToken, nil
	}
	t, err := exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "client_id": {r.ClientID}, "refresh_token": {r.RefreshToken}, "resource": {resource}})
	if err != nil {
		return "", err
	}
	if t.AccessToken == "" || t.RefreshToken == "" {
		return "", errors.New("incomplete refresh response")
	}
	r.AccessToken = t.AccessToken
	r.RefreshToken = t.RefreshToken
	r.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	if t.Scope != "" {
		r.Scope = t.Scope
	}
	if t.IDToken != "" {
		r.IDToken = t.IDToken
	}
	if err = write(r); err != nil {
		return "", err
	}
	return r.AccessToken, nil
}

func Disconnect(ctx context.Context) error {
	mu.Lock()
	defer mu.Unlock()
	r, err := read()
	if err != nil {
		return err
	}
	confirmed := true
	if r.RefreshToken != "" {
		confirmed = revoke(ctx, r) == nil
	}
	r.AccessToken = ""
	r.RefreshToken = ""
	r.IDToken = ""
	r.Scope = ""
	r.ExpiresAt = time.Time{}
	if err = write(r); err != nil {
		return err
	}
	if !confirmed {
		lastError = "本机已断开；远端撤销未确认，可在 ChatGPT 设置中移除授权。"
	} else {
		lastError = ""
	}
	return nil
}

func revoke(ctx context.Context, r record) error {
	req, err := http.NewRequestWithContext(ctx, "GET", authBase+"/.well-known/openid-configuration", nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("OIDC discovery returned %d", res.StatusCode)
	}
	var doc struct {
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return err
	}
	u, err := url.Parse(doc.RevocationEndpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() != "auth.openai.com" {
		return errors.New("untrusted revocation URL")
	}
	form := url.Values{"token": {r.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {r.ClientID}}
	req, err = http.NewRequestWithContext(ctx, "POST", doc.RevocationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err = client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("revocation returned %d", res.StatusCode)
	}
	return nil
}

// CurrentModelCatalog returns account-specific display models without exposing tokens.
func CurrentModelCatalog(ctx context.Context) ([]string, error) {
	token, err := AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", resource+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("model catalog returned %d", res.StatusCode)
	}
	var raw struct {
		Models []struct{ Slug, Visibility string }
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw.Models))
	for _, m := range raw.Models {
		if m.Slug != "" && m.Visibility == "list" {
			out = append(out, m.Slug)
		}
	}
	return out, nil
}
