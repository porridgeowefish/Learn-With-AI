package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const giteeReleases = "https://gitee.com/szu-andrew/learn-with-ai/releases"

const giteeReleaseAPI = "https://gitee.com/api/v5/repos/szu-andrew/learn-with-ai/releases"

var releaseAPI = giteeReleaseAPI + "?per_page=100"

type UpdateInfo struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	Available       bool   `json:"available"`
	NotesURL        string `json:"notesUrl,omitempty"`
	Error           string `json:"error,omitempty"`
	ReleaseNotes    string `json:"releaseNotes,omitempty"`
	DownloadURL     string `json:"downloadUrl,omitempty"`
	CanInstall      bool   `json:"canInstall"`
	Phase           string `json:"phase"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	InstallResult   string `json:"installResult,omitempty"`
	Multipart       bool   `json:"multipart,omitempty"`
	parts           []releaseAsset
}

type release struct {
	ID      int64          `json:"id"`
	Tag     string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Draft   bool           `json:"draft"`
	Pre     bool           `json:"prerelease"`
	Body    string         `json:"body"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type updater struct {
	current         string
	client          *http.Client
	mu              sync.Mutex
	checked         time.Time
	info            UpdateInfo
	downloadCancel  context.CancelFunc
	prepared        string
	preparedHash    string
	preparedVersion string
	cacheDir        string
	install         func(string) error
	shutdown        func()
	downloadClient  *http.Client
	downloads       sync.WaitGroup
	validate        func(string, string) error
	closed          bool
}

func newUpdater(current string) *updater {
	u := &updater{current: current, client: &http.Client{Timeout: 12 * time.Second}, install: launchUpdateInstaller, validate: validateUpdateInstaller}
	u.downloadClient = &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || !allowedUpdateRedirect(r.URL, via) {
			return errors.New("更新下载重定向不受信任")
		}
		return nil
	}}
	u.info = UpdateInfo{Current: current, NotesURL: giteeReleases, Phase: "idle", CanInstall: installedUpdateSupported()}
	u.info.InstallResult = readUpdateResult()
	return u
}

func parseVersion(raw string) ([3]int, bool) {
	var out [3]int
	core := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(raw), "v"), "-", 2)[0]
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return out, false
		}
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
	if u.info.Phase == "downloading" || u.info.Phase == "ready" || u.info.Phase == "installing" || (!force && !u.checked.IsZero() && time.Since(u.checked) < 30*time.Minute) {
		return u.info
	}
	info := UpdateInfo{Current: u.current, NotesURL: giteeReleases, Phase: "idle", CanInstall: installedUpdateSupported()}
	info.InstallResult = u.info.InstallResult
	rel, err := u.fetchRelease(ctx)
	if err != nil {
		info.Error = err.Error()
		u.info = info
		return info
	}
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.NotesURL = giteeReleases + "/tag/" + url.PathEscape(rel.Tag)
	info.ReleaseNotes = rel.Body
	for _, asset := range rel.Assets {
		if asset.Name == updateAssetName(rel.Tag, runtime.GOOS, runtime.GOARCH) && allowedUpdateURL(asset.URL) {
			info.DownloadURL = asset.URL
			info.TotalBytes = asset.Size
			break
		}
	}
	if info.DownloadURL == "" && runtime.GOOS == "windows" {
		name := updateAssetName(rel.Tag, runtime.GOOS, runtime.GOARCH)
		marker := regexp.MustCompile(`<!-- LLL-UPDATE-PARTS: ` + regexp.QuoteMeta(name) + ` ([0-9]+) ([0-9]+) -->`).FindStringSubmatch(rel.Body)
		if len(marker) == 3 {
			count, _ := strconv.Atoi(marker[1])
			total, _ := strconv.ParseInt(marker[2], 10, 64)
			if count >= 2 && count <= 16 && total > 0 && total <= maxUpdateBytes {
				for i := 1; i <= count; i++ {
					wanted := fmt.Sprintf("%s.part%03d", name, i)
					var found *releaseAsset
					for j := range rel.Assets {
						if rel.Assets[j].Name == wanted {
							found = &rel.Assets[j]
							break
						}
					}
					if found == nil || !allowedUpdateURL(found.URL) || found.Size <= 0 {
						info.parts = nil
						break
					}
					info.parts = append(info.parts, *found)
				}
				var sum int64
				for _, p := range info.parts {
					sum += p.Size
				}
				if len(info.parts) == count && sum == total {
					info.Multipart = true
					info.DownloadURL = info.NotesURL
					info.TotalBytes = total
				} else {
					info.parts = nil
				}
			}
		}
	}
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
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Learn-With-AI/"+u.current)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 Gitee 检查更新：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("检查更新失败：Gitee 返回 %d", resp.StatusCode)
	}
	var releases []release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&releases); err != nil {
		return nil, err
	}
	var selected *release
	for i := range releases {
		rel := &releases[i]
		if _, ok := parseVersion(rel.Tag); !ok || rel.Draft || rel.Pre || strings.ContainsAny(rel.Tag, "-+") {
			continue
		}
		if selected == nil || newerVersion(rel.Tag, selected.Tag) {
			selected = rel
		}
	}
	if selected == nil {
		return nil, errors.New("Gitee 暂无可用的正式发行版，请稍后重试")
	}
	// The release list only includes attachment names and URLs. The public
	// attachment endpoint supplies the sizes required to validate multipart MSI.
	if selected.ID > 0 {
		endpoint := fmt.Sprintf("%s/%d/attach_files?per_page=100", giteeReleaseAPI, selected.ID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Learn-With-AI/"+u.current)
		resp, err := u.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("无法读取 Gitee 发行附件：%w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("读取 Gitee 发行附件失败：%d", resp.StatusCode)
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&selected.Assets); err != nil {
			return nil, fmt.Errorf("无法解析 Gitee 发行附件：%w", err)
		}
	}
	return selected, nil
}

func (u *updater) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/system/update", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, u.check(r.Context(), r.URL.Query().Get("force") == "1"))
	})
	mux.HandleFunc("POST /api/system/update/prepare", func(w http.ResponseWriter, r *http.Request) {
		u.action(w, r, "prepare")
	})
	mux.HandleFunc("POST /api/system/update/install", func(w http.ResponseWriter, r *http.Request) {
		u.action(w, r, "install")
	})
	mux.HandleFunc("POST /api/system/update/cancel", func(w http.ResponseWriter, r *http.Request) { u.action(w, r, "cancel") })
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
