package promptpack

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const CacheTTL = 3 * time.Hour

var ErrUnauthorized = errors.New("cloud prompt pack requires login")

type Client struct {
	endpoint url.URL
	public   ed25519.PublicKey
	http     *http.Client
	now      func() time.Time
	mu       sync.Mutex
	epoch    uint64
	sequence uint64
	entries  map[cacheKey]cached
}

type cacheKey struct{ account, scene string }
type cached struct {
	pack     Pack
	until    time.Time
	sequence uint64
}

func NewClient(endpoint *url.URL, public ed25519.PublicKey, httpClient *http.Client) *Client {
	return &Client{
		endpoint: *endpoint, public: append(ed25519.PublicKey(nil), public...),
		http: httpClient, now: time.Now, entries: make(map[cacheKey]cached),
	}
}

// Clear invalidates cached content and prevents in-flight fetches from
// populating a subsequent signed-in session.
func (c *Client) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	c.epoch++
}

func (c *Client) Fetch(ctx context.Context, account, token, scene string) (Content, error) {
	if account == "" || scene == "" {
		return Content{}, errors.New("prompt packs require a subject and scene")
	}
	key := cacheKey{account, scene}
	c.mu.Lock()
	entry, known := c.entries[key]
	if scene != "teacher/general" && known && c.now().Before(entry.until) {
		c.mu.Unlock()
		return copyContent(entry.pack.Content), nil
	}
	epoch := c.epoch
	c.sequence++
	sequence := c.sequence
	c.mu.Unlock()
	// Refresh the teacher selection before the next turn, including across devices.
	endpoint := c.endpoint.JoinPath("v1", "prompts")
	query := endpoint.Query()
	query.Set("scene", scene)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return Content{}, err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if known {
		request.Header.Set("If-None-Match", entry.pack.ETag())
	}
	response, err := c.http.Do(request)
	if err != nil {
		return c.cachedAfterFailure(key, epoch, err)
	}
	defer response.Body.Close()
	var pack Pack
	switch {
	case response.StatusCode == http.StatusNotModified && known:
		pack = entry.pack
	case response.StatusCode == http.StatusOK:
		data, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
		if err != nil || len(data) > MaxResponseBytes {
			return Content{}, errors.New("invalid prompt pack response")
		}
		if err := json.Unmarshal(data, &pack); err != nil {
			return Content{}, err
		}
		if err := pack.Verify(c.public); err != nil {
			return Content{}, err
		}
		if pack.Content.Scene != "" && pack.Content.Scene != scene {
			return Content{}, errors.New("prompt pack was signed for a different scene")
		}
		pack.Content.PackDigest = pack.Digest
	case response.StatusCode == http.StatusUnauthorized:
		return Content{}, ErrUnauthorized
	case response.StatusCode >= http.StatusInternalServerError:
		return c.cachedAfterFailure(key, epoch, errors.New("cloud prompt pack unavailable"))
	default:
		return Content{}, errors.New("cloud prompt pack unavailable")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if epoch != c.epoch {
		return Content{}, errors.New("prompt cache was cleared")
	}
	if latest := c.entries[key]; latest.sequence > sequence {
		return copyContent(latest.pack.Content), nil
	}
	c.entries[key] = cached{pack: pack, until: c.now().Add(CacheTTL), sequence: sequence}
	return copyContent(pack.Content), nil
}

func (c *Client) cachedAfterFailure(key cacheKey, epoch uint64, cause error) (Content, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, known := c.entries[key]; epoch == c.epoch && known && c.now().Before(entry.until) {
		return copyContent(entry.pack.Content), nil
	}
	return Content{}, cause
}

func copyContent(content Content) Content {
	content.Resources = maps.Clone(content.Resources)
	return content
}
