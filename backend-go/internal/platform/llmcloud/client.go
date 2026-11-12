// Package llmcloud is the desktop's authenticated connection to Learn-With-AI cloud services.
package llmcloud

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/platform/envname"
	"github.com/xmz14/lll/backend-go/internal/platform/netproxy"
	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

const DefaultEndpoint = "https://43.136.105.53"

// PromptPublicKey pins the public signing key of DefaultEndpoint.
// A release build can override it during a coordinated key rotation.
var PromptPublicKey = "YAqxuxSHypMQu6BY0C4EZgR6LRaPo9Os+FjFEp15pMg="

var ErrLoginRequired = errors.New("login_required")

type RemoteError struct {
	Status           int
	Code             string
	CreditSettlement *CreditSettlement
}

func (e *RemoteError) Error() string { return e.Code }

type CreditSettlement struct {
	Units     int64  `json:"units"`
	Pending   bool   `json:"pending"`
	RequestID string `json:"-"`
}

type Account struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name,omitempty"`
	Balance      int64  `json:"balance"`
	GitHubLinked bool   `json:"githubLinked,omitempty"`
}

type Wallet struct {
	Balance int64 `json:"balance"`
	Held    int64 `json:"held"`
	Granted bool  `json:"granted,omitempty"`
}

type DailyClaimHistory struct {
	Today   string `json:"today"`
	Entries []struct {
		Day   string `json:"day"`
		Units int64  `json:"units"`
	} `json:"entries"`
}

type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MaxInput  int    `json:"maxInput"`
	MaxOutput int    `json:"maxOutput"`
}

type GuestModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type session struct {
	Token     string    `json:"token"`
	Account   Account   `json:"account"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Client struct {
	endpoint       *url.URL
	http           *http.Client
	prompts        *promptpack.Client
	mu             sync.RWMutex
	session        session
	sessionVersion uint64
	selected       Model
	catalog        []Model
	catalogExpires time.Time
	vault          *sessionVault
}

func New(endpoint string, public ed25519.PublicKey, transport http.RoundTripper) (*Client, error) {
	root, err := url.Parse(endpoint)
	if err != nil || root.Host == "" || root.User != nil || root.RawQuery != "" || root.Fragment != "" ||
		(root.Scheme != "https" && !(root.Scheme == "http" && root.Hostname() == "127.0.0.1")) {
		return nil, errors.New("invalid Learn-With-AI cloud endpoint")
	}
	httpClient := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client := &Client{endpoint: root, http: httpClient}
	if len(public) == ed25519.PublicKeySize {
		client.prompts = promptpack.NewClient(root, public, httpClient)
	}
	return client, nil
}

var defaultClient = sync.OnceValues(func() (*Client, error) {
	endpoint := envname.Value("LWA_CLOUD_URL")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	key := envname.Value("LWA_CLOUD_PROMPT_PUBLIC_KEY")
	if key == "" {
		key = PromptPublicKey
	}
	decoded, _ := base64.StdEncoding.DecodeString(key)
	client, err := New(endpoint, decoded, netproxy.Transport())
	if err == nil && endpoint == DefaultEndpoint &&
		!strings.HasSuffix(strings.ToLower(os.Args[0]), ".test.exe") && !strings.HasSuffix(os.Args[0], ".test") {
		client.vault = newSessionVault()
		if client.vault != nil {
			client.session, client.selected = client.vault.load()
		}
	}
	return client, err
})

func Default() (*Client, error) { return defaultClient() }

func (c *Client) Current() (Account, bool) {
	s := c.currentSession()
	return s.Account, s.Token != "" && time.Now().Before(s.ExpiresAt)
}

func (c *Client) currentSession() session {
	c.mu.RLock()
	current := c.session
	c.mu.RUnlock()
	if current.Token != "" && !time.Now().Before(current.ExpiresAt) {
		c.clearSession(current.Token)
		return session{}
	}
	return current
}

func (c *Client) setSession(next session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setSessionLocked(next)
}

func (c *Client) setSessionLocked(next session) {
	if next.Account.ID != c.session.Account.ID || next.Token == "" {
		c.selected = Model{}
		c.catalog = nil
		c.catalogExpires = time.Time{}
		if c.prompts != nil {
			c.prompts.Clear()
		}
	}
	c.session = next
	c.sessionVersion++
	c.persistLocked()
}

func (c *Client) persistLocked() {
	if c.vault != nil {
		c.vault.save(c.session, c.selected)
	}
}

func (c *Client) clearSession(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if token != "" && c.session.Token == token {
		c.setSessionLocked(session{})
	}
}

func (c *Client) sessionError(token string, err error) error {
	var remote *RemoteError
	if token != "" && errors.As(err, &remote) && remote.Status == http.StatusUnauthorized && remote.Code == "login_required" {
		c.clearSession(token)
		return ErrLoginRequired
	}
	return err
}

func (c *Client) SendEmail(ctx context.Context, email, purpose string) error {
	_, err := c.json(ctx, http.MethodPost, "v1/auth/email/send", "", map[string]string{"email": email, "purpose": purpose}, nil)
	return err
}

func (c *Client) VerifyEmail(ctx context.Context, email, purpose, code, username, password string) (Account, error) {
	return c.signIn(ctx, "v1/auth/email/verify", map[string]string{
		"email": email, "purpose": purpose, "code": code, "username": username, "password": password,
	})
}

func (c *Client) SendBindingEmail(ctx context.Context, email string) error {
	current := c.currentSession()
	if current.Token == "" || !time.Now().Before(current.ExpiresAt) {
		return ErrLoginRequired
	}
	_, err := c.json(ctx, http.MethodPost, "v1/auth/email/bind/send", current.Token, map[string]string{"email": email}, nil)
	return err
}

func (c *Client) BindEmail(ctx context.Context, email, code, password string) (Account, error) {
	current := c.currentSession()
	if current.Token == "" || !time.Now().Before(current.ExpiresAt) {
		return Account{}, ErrLoginRequired
	}
	var account Account
	_, err := c.json(ctx, http.MethodPost, "v1/auth/email/bind/verify", current.Token, map[string]string{
		"email": email, "code": code, "password": password,
	}, &account)
	if err != nil {
		return Account{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session.Token != current.Token || account.ID != c.session.Account.ID {
		return Account{}, ErrLoginRequired
	}
	c.session.Account = account
	c.persistLocked()
	return account, nil
}

func (c *Client) UnlinkIdentity(ctx context.Context, provider, password string) (Account, error) {
	if provider != "email" && provider != "github" {
		return Account{}, errors.New("unsupported cloud identity")
	}
	c.mu.RLock()
	current, version := c.session, c.sessionVersion
	c.mu.RUnlock()
	if current.Token == "" || !time.Now().Before(current.ExpiresAt) {
		return Account{}, ErrLoginRequired
	}
	var account Account
	payload := map[string]string{}
	if provider == "github" {
		payload["password"] = password
	}
	_, err := c.json(ctx, http.MethodPost, "v1/auth/"+provider+"/unbind", current.Token, payload, &account)
	if err != nil {
		return Account{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionVersion != version || account.ID != current.Account.ID {
		return Account{}, ErrLoginRequired
	}
	current.Account = account
	c.setSessionLocked(current)
	return account, nil
}

func (c *Client) LoginPassword(ctx context.Context, username, password string) (Account, error) {
	return c.signIn(ctx, "v1/auth/password", map[string]string{"username": username, "password": password})
}

func (c *Client) SetLoginCredentials(ctx context.Context, accountID, username, password, confirmation string) (Account, error) {
	current := c.currentSession()
	if current.Token == "" || accountID == "" || current.Account.ID != accountID || !time.Now().Before(current.ExpiresAt) {
		return Account{}, ErrLoginRequired
	}
	var account Account
	_, err := c.json(ctx, http.MethodPost, "v1/account/credentials", current.Token, map[string]string{
		"username": username, "password": password, "confirmPassword": confirmation,
	}, &account)
	if err != nil {
		return Account{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session.Token != current.Token || account.ID != accountID {
		return Account{}, ErrLoginRequired
	}
	c.session.Account = account
	c.persistLocked()
	return account, nil
}

func (c *Client) signIn(ctx context.Context, path string, payload any) (Account, error) {
	var next session
	if _, err := c.json(ctx, http.MethodPost, path, "", payload, &next); err != nil {
		return Account{}, err
	}
	if next.Token == "" || next.Account.ID == "" || !time.Now().Before(next.ExpiresAt) {
		return Account{}, errors.New("invalid cloud session response")
	}
	c.setSession(next)
	return next.Account, nil
}

type GitHubFlow struct {
	ID, AuthorizationURL string
	Verifier             string
	link                 bool
	sessionVersion       uint64
}

func (c *Client) StartGitHub(ctx context.Context, link bool) (GitHubFlow, error) {
	var secret [32]byte
	_, _ = rand.Read(secret[:])
	verifier := base64.RawURLEncoding.EncodeToString(secret[:])
	proof := sha256.Sum256([]byte(verifier))
	var result struct{ FlowID, AuthorizationURL string }
	c.mu.RLock()
	current, version := c.session, c.sessionVersion
	c.mu.RUnlock()
	var token string
	if link {
		token = current.Token
		if token == "" || !time.Now().Before(current.ExpiresAt) {
			return GitHubFlow{}, ErrLoginRequired
		}
	}
	_, err := c.json(ctx, http.MethodPost, "v1/auth/github/start", token, map[string]any{
		"challenge": base64.RawURLEncoding.EncodeToString(proof[:]), "link": link,
	}, &result)
	if err != nil {
		return GitHubFlow{}, err
	}
	authURL, err := url.Parse(result.AuthorizationURL)
	if err != nil || authURL.Scheme != "https" || authURL.Host != "github.com" || authURL.User != nil ||
		authURL.Path != "/login/oauth/authorize" || result.FlowID == "" {
		return GitHubFlow{}, errors.New("invalid GitHub authorization response")
	}
	return GitHubFlow{ID: result.FlowID, AuthorizationURL: result.AuthorizationURL, Verifier: verifier, link: link, sessionVersion: version}, nil
}

func (c *Client) SendGitHubConfirmation(ctx context.Context, f GitHubFlow) error {
	c.mu.RLock()
	version := c.sessionVersion
	c.mu.RUnlock()
	if f.link || version != f.sessionVersion {
		return ErrLoginRequired
	}
	_, err := c.json(ctx, http.MethodPost, "v1/auth/github/email/send", "", map[string]string{"flowId": f.ID, "verifier": f.Verifier}, nil)
	return err
}

func (c *Client) FinishGitHub(ctx context.Context, f GitHubFlow, emailCode ...string) (Account, bool, error) {
	c.mu.RLock()
	current, version := c.session, c.sessionVersion
	c.mu.RUnlock()
	if version != f.sessionVersion || (f.link && !time.Now().Before(current.ExpiresAt)) {
		return Account{}, false, ErrLoginRequired
	}
	var token string
	if f.link {
		token = current.Token
	}
	var next session
	input := map[string]string{"flowId": f.ID, "verifier": f.Verifier}
	if len(emailCode) > 0 && emailCode[0] != "" {
		input["code"] = emailCode[0]
	}
	status, err := c.json(ctx, http.MethodPost, "v1/auth/github/finish", token, input, &next)
	if err != nil || status == http.StatusAccepted {
		return Account{}, false, err
	}
	if next.Token == "" || next.Account.ID == "" || !time.Now().Before(next.ExpiresAt) {
		return Account{}, false, errors.New("invalid cloud session response")
	}
	if f.link && (next.Token != current.Token || next.Account.ID != current.Account.ID) {
		return Account{}, false, errors.New("invalid GitHub binding response")
	}
	c.mu.Lock()
	if c.sessionVersion != f.sessionVersion {
		c.mu.Unlock()
		if !f.link {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer cancel()
			_, _ = c.json(cleanup, http.MethodPost, "v1/auth/logout", next.Token, nil, nil)
		}
		return Account{}, false, ErrLoginRequired
	}
	c.setSessionLocked(next)
	c.mu.Unlock()
	return next.Account, true, nil
}

func (c *Client) Wallet(ctx context.Context) (Account, Wallet, bool, error) {
	var result struct {
		Account      Account
		Wallet       Wallet
		DailyClaimed bool
	}
	err := c.authJSON(ctx, http.MethodGet, "v1/account", &result)
	return result.Account, result.Wallet, result.DailyClaimed, err
}

func (c *Client) ClaimDaily(ctx context.Context) (Wallet, error) {
	var wallet Wallet
	err := c.authJSON(ctx, http.MethodPost, "v1/credits/daily", &wallet)
	return wallet, err
}

func (c *Client) DailyClaims(ctx context.Context) (DailyClaimHistory, error) {
	var history DailyClaimHistory
	err := c.authJSON(ctx, http.MethodGet, "v1/credits/daily/history", &history)
	return history, err
}

func (c *Client) SettledCredits(ctx context.Context, requestIDs []string) (map[string]int64, error) {
	s := c.currentSession()
	if s.Token == "" {
		return nil, ErrLoginRequired
	}
	var settled map[string]int64
	_, err := c.json(ctx, http.MethodPost, "v1/credits/settlements", s.Token, map[string][]string{"requestIds": requestIDs}, &settled)
	return settled, err
}

func (c *Client) RateTeaching(ctx context.Context, exposureID string, stars int) error {
	s := c.currentSession()
	if s.Token == "" {
		return ErrLoginRequired
	}
	_, err := c.json(ctx, http.MethodPost, "v1/experiments/teaching-rating", s.Token,
		struct {
			ExposureID string `json:"exposureId"`
			Stars      int    `json:"stars"`
		}{exposureID, stars}, nil)
	return c.sessionError(s.Token, err)
}

func (c *Client) Models(ctx context.Context) ([]Model, error) {
	c.mu.RLock()
	version := c.sessionVersion
	c.mu.RUnlock()
	var models []Model
	_, err := c.json(ctx, http.MethodGet, "v1/models", "", nil, &models)
	if err == nil {
		c.mu.Lock()
		if c.sessionVersion == version && c.session.Token != "" && time.Now().Before(c.session.ExpiresAt) {
			c.catalog = append([]Model(nil), models...)
			c.catalogExpires = time.Now().Add(time.Minute)
		}
		c.mu.Unlock()
	}
	return models, err
}

// ModelForConversation resolves an explicit cloud model without changing the selected default.
func (c *Client) ModelForConversation(ctx context.Context, id string) Model {
	c.mu.RLock()
	version := c.sessionVersion
	active := c.session.Token != "" && time.Now().Before(c.session.ExpiresAt)
	fresh := time.Now().Before(c.catalogExpires)
	models := c.catalog
	c.mu.RUnlock()
	if !active {
		return Model{}
	}
	if !fresh {
		if _, err := c.Models(ctx); err != nil {
			return Model{}
		}
		c.mu.RLock()
		models = c.catalog
		c.mu.RUnlock()
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.sessionVersion != version || !time.Now().Before(c.session.ExpiresAt) {
		return Model{}
	}
	for _, model := range models {
		if model.ID == id {
			return model
		}
	}
	return Model{}
}

func (c *Client) GuestModels(ctx context.Context) ([]GuestModel, error) {
	var models []GuestModel
	_, err := c.json(ctx, http.MethodGet, "v1/models/guest", "", nil, &models)
	return models, err
}

func (c *Client) SelectModel(ctx context.Context, id string) error {
	if id == "" {
		c.mu.Lock()
		c.selected = Model{}
		c.persistLocked()
		c.mu.Unlock()
		return nil
	}
	session := c.currentSession()
	if session.Token == "" || !time.Now().Before(session.ExpiresAt) {
		return ErrLoginRequired
	}
	models, err := c.Models(ctx)
	if err != nil {
		return err
	}
	for _, model := range models {
		if model.ID == id {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.session.Token != session.Token {
				return ErrLoginRequired
			}
			c.selected = model
			c.persistLocked()
			return nil
		}
	}
	return errors.New("system_model_unavailable")
}

func (c *Client) Selected() Model {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !time.Now().Before(c.session.ExpiresAt) {
		return Model{}
	}
	return c.selected
}

func (c *Client) Logout(ctx context.Context) error {
	c.mu.Lock()
	old := c.session
	c.setSessionLocked(session{})
	c.mu.Unlock()
	if old.Token == "" {
		return nil
	}
	_, err := c.json(ctx, http.MethodPost, "v1/auth/logout", old.Token, nil, nil)
	return err
}

func (c *Client) Prompts(ctx context.Context, scene string) (promptpack.Content, error) {
	s := c.currentSession()
	if c.prompts == nil || s.Token == "" || !time.Now().Before(s.ExpiresAt) {
		return promptpack.Content{}, ErrLoginRequired
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	content, err := c.prompts.Fetch(ctx, s.Account.ID, s.Token, scene)
	if errors.Is(err, promptpack.ErrUnauthorized) {
		c.clearSession(s.Token)
		return promptpack.Content{}, ErrLoginRequired
	}
	return content, err
}

func (c *Client) Stream(ctx context.Context, payload any, emit func(json.RawMessage) error) error {
	s := c.currentSession()
	if s.Token == "" || !time.Now().Before(s.ExpiresAt) {
		return ErrLoginRequired
	}
	response, err := c.request(ctx, http.MethodPost, "v1/inference", s.Token, payload)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return c.sessionError(s.Token, remoteError(response))
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if data, found := bytes.CutPrefix(line, []byte("data:")); found {
			var meta struct{ Type, Code string }
			if err := json.Unmarshal(data, &meta); err != nil {
				return err
			}
			switch meta.Type {
			case "done":
				return nil
			case "error":
				return &RemoteError{Status: http.StatusBadGateway, Code: meta.Code}
			default:
				if err := emit(bytes.TrimSpace(data)); err != nil {
					return err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func (c *Client) authJSON(ctx context.Context, method, path string, result any) error {
	s := c.currentSession()
	if s.Token == "" || !time.Now().Before(s.ExpiresAt) {
		return ErrLoginRequired
	}
	_, err := c.json(ctx, method, path, s.Token, nil, result)
	return err
}

func (c *Client) json(ctx context.Context, method, path, token string, payload, result any) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	response, err := c.request(ctx, method, path, token, payload)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	status := response.StatusCode
	if status < 200 || status >= 300 {
		return status, c.sessionError(token, remoteError(response))
	}
	if result != nil && status != http.StatusAccepted && status != http.StatusNoContent {
		return status, json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result)
	}
	return status, nil
}

func (c *Client) request(ctx context.Context, method, path, token string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint.JoinPath(path).String(), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return c.http.Do(request)
}

func remoteError(response *http.Response) error {
	var payload struct {
		Error            string
		CreditSettlement *CreditSettlement `json:"creditSettlement"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload)
	if payload.Error == "" || len(payload.Error) > 80 || strings.ContainsAny(payload.Error, "\r\n") {
		payload.Error = "cloud_request_failed"
	}
	return &RemoteError{Status: response.StatusCode, Code: payload.Error, CreditSettlement: payload.CreditSettlement}
}
