package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The desktop app checks the latest official release and directs users to
// GitHub to download and install it themselves.
var releaseAPI = "https://api.github.com/repos/porridgeowefish/Learn-With-AI/releases/latest"

type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	NotesURL  string `json:"notesUrl,omitempty"`
	Error     string `json:"error,omitempty"`
}

type release struct {
	Tag     string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Pre     bool   `json:"prerelease"`
}

type updater struct {
	current string
	client  *http.Client
	mu      sync.Mutex
	checked time.Time
	info    UpdateInfo
}

func newUpdater(current string) *updater {
	return &updater{current: current, client: &http.Client{Timeout: 8 * time.Second}}
}

func parseVersion(raw string) ([3]int, bool) {
	var out [3]int
	core := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(raw), "v"), "-", 2)[0]
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func newerVersion(candidate, current string) bool {
	a, ok := parseVersion(candidate)
	b, okCurrent := parseVersion(current)
	if !ok || !okCurrent {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func (u *updater) check(ctx context.Context, force bool) UpdateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !force && time.Since(u.checked) < 30*time.Minute && u.info.Current != "" {
		return u.info
	}
	info := UpdateInfo{Current: u.current}
	rel, err := u.fetchRelease(ctx)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.NotesURL = rel.HTMLURL
	info.Available = newerVersion(rel.Tag, u.current)
	u.checked, u.info = time.Now(), info
	return info
}

func (u *updater) fetchRelease(ctx context.Context) (*release, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Learn-With-AI/"+u.current)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 GitHub 检查更新：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("检查更新失败：GitHub 返回 %d", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.Draft || rel.Pre || rel.Tag == "" {
		return nil, errors.New("没有可用的正式版本")
	}
	return &rel, nil
}

func (u *updater) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/system/update", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, u.check(r.Context(), r.URL.Query().Get("force") == "1"))
	})
	// Older frontend bundles may still call these retired endpoints.
	mux.HandleFunc("POST /api/system/update/prepare", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Download the latest release from GitHub", http.StatusGone)
	})
	mux.HandleFunc("POST /api/system/update/install", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Download the latest release from GitHub", http.StatusGone)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
