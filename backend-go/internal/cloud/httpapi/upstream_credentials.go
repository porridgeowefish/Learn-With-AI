package httpapi

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

var upstreamEnvName = regexp.MustCompile(`^(LWA|LLL)_CLOUD_UPSTREAM_[A-Za-z0-9_]+$`)
var credentialID = regexp.MustCompile(`^key-[a-z0-9]{26}$`)

type UpstreamCredentials struct {
	db    *store.Store
	vault cipher.AEAD
}

func NewUpstreamCredentials(db *store.Store, masterKey []byte) (*UpstreamCredentials, error) {
	catalog := &UpstreamCredentials{db: db}
	if len(masterKey) == 0 {
		return catalog, nil
	}
	if len(masterKey) != 32 {
		return nil, errors.New("LWA_CLOUD_CREDENTIAL_KEY must encode 32 random bytes")
	}
	key, err := hkdf.Key(sha256.New, masterKey, nil, "Learn-With-AI upstream API credentials", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	catalog.vault, err = cipher.NewGCMWithRandomNonce(block)
	return catalog, err
}

func credentialBinding(id, host string) []byte { return []byte(id + "\x00" + host) }

func (c *UpstreamCredentials) Resolve(ctx context.Context, id, host string) (string, error) {
	if c.vault == nil {
		return "", errors.New("upstream credential storage is disabled")
	}
	entry, err := c.db.UpstreamCredential(ctx, id)
	if err != nil {
		return "", errors.New("saved API credential is unavailable")
	}
	if entry.Host != "" && entry.Host != host {
		return "", errors.New("saved API credential belongs to another upstream host")
	}
	plain, err := c.vault.Open(nil, nil, entry.Encrypted, credentialBinding(id, entry.Host))
	if err != nil {
		return "", errors.New("saved API credential cannot be decrypted")
	}
	return string(plain), nil
}

func (s *Server) EnableUpstreamCredentialAdmin(c *UpstreamCredentials) {
	type entry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Host string `json:"host,omitempty"`
	}
	s.mux.HandleFunc("GET /v1/admin/upstream-credentials", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, true); !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		saved, err := c.db.ListUpstreamCredentials(r.Context())
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "upstream_credentials_unavailable")
			return
		}
		items := make([]entry, 0, len(saved))
		for _, item := range saved {
			items = append(items, entry{item.ID, item.Name, item.Host})
		}
		env := []string{}
		for _, variable := range os.Environ() {
			name, value, _ := strings.Cut(variable, "=")
			if value != "" && upstreamEnvName.MatchString(name) {
				env = append(env, name)
			}
		}
		slices.Sort(env)
		respond(w, http.StatusOK, map[string]any{
			"enabled": c.vault != nil, "credentials": items, "environment": env,
		})
	})
	s.mux.HandleFunc("POST /v1/admin/upstream-credentials", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := s.adminAuthorize(w, r, true)
		if !ok || !s.allowAuth(w, r, "upstream-credential-add", admin.ID, 20) {
			return
		}
		if c.vault == nil {
			fail(w, http.StatusServiceUnavailable, "upstream_credentials_disabled")
			return
		}
		var input struct{ Name, Host, Secret string }
		if !decode(w, r, &input, 8<<10) {
			return
		}
		input.Name = strings.TrimSpace(input.Name)
		input.Host = strings.ToLower(strings.TrimSpace(input.Host))
		if input.Name == "" || len(input.Name) > 160 || strings.Contains(input.Name, input.Secret) ||
			strings.IndexFunc(input.Name, unicode.IsControl) >= 0 || len(input.Secret) < 8 || len(input.Secret) > 4096 ||
			strings.IndexFunc(input.Secret, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 ||
			strings.ContainsAny(input.Host, "/:@?#") || strings.IndexFunc(input.Host, unicode.IsSpace) >= 0 {
			fail(w, http.StatusBadRequest, "invalid_upstream_credential")
			return
		}
		id := "key-" + strings.ToLower(rand.Text())
		encrypted := c.vault.Seal(nil, nil, []byte(input.Secret), credentialBinding(id, input.Host))
		if err := c.db.AddUpstreamCredential(r.Context(), input.Name, input.Host, encrypted, id); err != nil {
			if errors.Is(err, store.ErrConflict) {
				fail(w, http.StatusConflict, "upstream_credential_limit")
				return
			}
			fail(w, http.StatusServiceUnavailable, "upstream_credentials_unavailable")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		respond(w, http.StatusCreated, entry{id, input.Name, input.Host})
	})
	s.mux.HandleFunc("POST /v1/admin/upstream-credentials/remove", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, true); !ok {
			return
		}
		var input struct{ ID string }
		if !decode(w, r, &input, 1024) {
			return
		}
		if !credentialID.MatchString(input.ID) {
			fail(w, http.StatusBadRequest, "invalid_upstream_credential")
			return
		}
		if !s.modelMu.TryLock() {
			fail(w, http.StatusConflict, "model_configuration_busy")
			return
		}
		defer s.modelMu.Unlock()
		switch err := c.db.RemoveUpstreamCredential(r.Context(), input.ID); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, store.ErrConflict):
			fail(w, http.StatusConflict, "upstream_credential_in_use")
		case errors.Is(err, store.ErrNotFound):
			fail(w, http.StatusNotFound, "upstream_credential_not_found")
		default:
			fail(w, http.StatusServiceUnavailable, "upstream_credentials_unavailable")
		}
	})
}
