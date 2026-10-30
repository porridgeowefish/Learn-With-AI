package desktop

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Releases are published by the repository's release workflow; the desktop app
// only ever installs an MSI from that release whose SHA256 matches the
// release's SHA256SUMS.
var releaseAPI = "https://api.github.com/repos/porridgeowefish/Learn-With-AI/releases/latest"

type UpdateInfo struct {
	Current         string         `json:"current"`
	Latest          string         `json:"latest,omitempty"`
	Available       bool           `json:"available"`
	CanInstall      bool           `json:"canInstall"`
	NotesURL        string         `json:"notesUrl,omitempty"`
	Asset           string         `json:"asset,omitempty"`
	Error           string         `json:"error,omitempty"`
	LastResult      *UpdateAttempt `json:"lastResult,omitempty"`
	Phase           string         `json:"phase"`
	Downloaded      int64          `json:"downloaded"`
	Total           int64          `json:"total"`
	PreparedVersion string         `json:"preparedVersion,omitempty"`
}

// UpdateAttempt is written by the installer helper after msiexec exits.
type UpdateAttempt struct {
	Version  string `json:"version"`
	ExitCode int    `json:"exitCode"`
	Log      string `json:"log"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type release struct {
	Tag     string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Draft   bool           `json:"draft"`
	Pre     bool           `json:"prerelease"`
	Assets  []releaseAsset `json:"assets"`
}

type updater struct {
	current         string
	workspace       string
	client          *http.Client
	mu              sync.Mutex
	checked         time.Time
	info            UpdateInfo
	release         *release
	installed       bool
	phase           string
	updateError     string
	preparedPath    string
	preparedVersion string
	downloaded      int64
	total           int64
	preparedSum     string
	launch          func(string, string, string) error
}

func newUpdater(current, workspace string) *updater {
	return &updater{current: current, workspace: workspace, phase: "idle", client: &http.Client{Timeout: 10 * time.Minute}, launch: launchInstaller}
}

// parseVersion accepts "v1.2.3" or "1.2.3-dev"; the pre-release suffix is ignored.
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

// newerVersion reports whether candidate is strictly newer than current.
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

func msiAssetName(version string) string {
	return fmt.Sprintf("learn-with-ai-%s-windows-%s.msi", strings.TrimPrefix(version, "v"), runtime.GOARCH)
}

func (u *updater) statusPath() string { return filepath.Join(u.workspace, ".update-result.json") }

func (u *updater) check(ctx context.Context, force bool) UpdateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !force && time.Since(u.checked) < 30*time.Minute && u.info.Current != "" {
		return u.withLastResult(u.info)
	}
	info := UpdateInfo{Current: u.current, CanInstall: runtime.GOOS == "windows"}
	rel, err := u.fetchRelease(ctx)
	if err != nil {
		info.Error = err.Error()
		return u.withLastResult(info)
	}
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.NotesURL = rel.HTMLURL
	info.Asset = msiAssetName(rel.Tag)
	info.Available = newerVersion(rel.Tag, u.current)
	u.checked, u.info, u.release = time.Now(), info, rel
	return u.withLastResult(info)
}

func (u *updater) withLastResult(info UpdateInfo) UpdateInfo {
	info.Phase, info.Downloaded, info.Total, info.PreparedVersion = u.phase, u.downloaded, u.total, u.preparedVersion
	if u.updateError != "" {
		info.Error = u.updateError
	}
	data, err := os.ReadFile(u.statusPath())
	if err == nil {
		var attempt UpdateAttempt
		if json.Unmarshal(data, &attempt) == nil {
			info.LastResult = &attempt
		}
	}
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

func (rel *release) asset(name string) (releaseAsset, bool) {
	for _, asset := range rel.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func trustedAssetURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	for _, repo := range []string{"Learn-With-AI", "LLL_life_long_learn"} {
		if strings.HasPrefix(parsed.Path, "/porridgeowefish/"+repo+"/releases/download/") && !strings.Contains(parsed.Path, "..") {
			return true
		}
	}
	return false
}

// download fetches the release MSI into a private temp folder and verifies it
// against the release's SHA256SUMS before anything is executed.
func (u *updater) download(ctx context.Context) (string, string, error) {
	u.mu.Lock()
	rel := u.release
	u.mu.Unlock()
	if rel == nil || !newerVersion(rel.Tag, u.current) {
		return "", "", errors.New("当前已是最新版本")
	}
	name := msiAssetName(rel.Tag)
	msi, ok := rel.asset(name)
	sums, okSums := rel.asset("SHA256SUMS")
	if !ok || !okSums {
		return "", "", fmt.Errorf("新版本缺少安装包 %s 或校验文件", name)
	}
	if !trustedAssetURL(msi.URL) || !trustedAssetURL(sums.URL) {
		return "", "", errors.New("更新下载地址不属于官方 GitHub Release")
	}
	expected, err := u.expectedSum(ctx, sums.URL, name)
	if err != nil {
		return "", "", err
	}
	dir, err := os.MkdirTemp("", "learn-with-ai-update-")
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(dir, name)
	if err := u.fetchTo(ctx, msi.URL, target, expected); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	return target, strings.TrimPrefix(rel.Tag, "v"), nil
}

func (u *updater) expectedSum(ctx context.Context, url, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载校验文件失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载校验文件失败：%d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == name && len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err != nil {
				return "", errors.New("发布校验摘要无效")
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("校验文件中没有 %s", name)
}

func (u *updater) fetchTo(ctx context.Context, url, target, expected string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("下载更新失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载更新失败：%d", resp.StatusCode)
	}
	u.mu.Lock()
	u.total, u.downloaded = resp.ContentLength, 0
	u.mu.Unlock()
	partial := target + ".partial"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	count, copyErr := io.Copy(io.MultiWriter(file, hash, updateProgress{u}), io.LimitReader(resp.Body, (512<<20)+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(partial)
		return fmt.Errorf("下载更新失败：%v", errors.Join(copyErr, closeErr))
	}
	if count > 512<<20 {
		_ = os.Remove(partial)
		return errors.New("更新安装包超过大小限制")
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != expected {
		os.Remove(partial)
		return errors.New("安装包校验失败，已丢弃下载的文件")
	}
	u.mu.Lock()
	u.preparedSum = expected
	u.mu.Unlock()
	os.Remove(target)
	return os.Rename(partial, target)
}

type updateProgress struct{ u *updater }

func (p updateProgress) Write(data []byte) (int, error) {
	p.u.mu.Lock()
	p.u.downloaded += int64(len(data))
	p.u.mu.Unlock()
	return len(data), nil
}

func (u *updater) prepare() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var path, version string
	var err error
	if info := u.check(ctx, true); info.Error != "" {
		err = errors.New(info.Error)
	} else {
		path, version, err = u.download(ctx)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		u.phase, u.updateError = "failed", err.Error()
		return
	}
	u.preparedPath, u.preparedVersion, u.phase = path, version, "ready"
}

// handler reports new releases and sends users to GitHub for installation.
// Former install endpoints stay unavailable for older frontend bundles.
func (u *updater) handler(_ string, _ func()) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/system/update", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		busy := u.phase == "downloading" || u.phase == "ready" || u.phase == "installing"
		info := u.withLastResult(u.info)
		info.Current, info.CanInstall = u.current, false
		u.mu.Unlock()
		if !busy {
			info = u.check(r.Context(), r.URL.Query().Get("force") == "1")
		}
		info.CanInstall = false
		writeJSON(w, http.StatusOK, info)
	})
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
