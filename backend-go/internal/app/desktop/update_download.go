package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/platform/workspacegate"
	"github.com/xmz14/lll/backend-go/internal/transport/httpserver"
)

const maxUpdateBytes int64 = 512 << 20

func updateAssetName(tag, platform, arch string) string {
	ext := ".msi"
	if platform == "darwin" {
		ext = ".pkg"
	} else if platform == "linux" {
		ext = "-install.tar.gz"
	}
	return "learn-with-ai-" + strings.TrimPrefix(tag, "v") + "-" + platform + "-" + arch + ext
}

func allowedUpdateURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host != "gitee.com" || u.User != nil || u.Fragment != "" {
		return false
	}
	return strings.HasPrefix(u.Path, "/szu-andrew/learn-with-ai/attach_files/") || strings.HasPrefix(u.Path, "/szu-andrew/learn-with-ai/releases/download/") || strings.HasPrefix(u.Path, "/szu-andrew/learn-with-ai/attachments/")
}

var giteeAttachmentPath = regexp.MustCompile(`^/szu-andrew/learn-with-ai/attach_files/[0-9]+/download/[^/]+$`)
var giteeCDNPath = regexp.MustCompile(`^/attach_file/[0-9]+/[^/]+$`)

func allowedUpdateRedirect(next *url.URL, via []*http.Request) bool {
	if allowedUpdateURL(next.String()) {
		return true
	}
	if len(via) < 1 || next.Scheme != "https" || next.Host != "foruda.gitee.com" || next.User != nil || next.Fragment != "" || !giteeCDNPath.MatchString(next.Path) {
		return false
	}
	first, previous := via[0].URL, via[len(via)-1].URL
	return allowedUpdateURL(first.String()) && allowedUpdateURL(previous.String()) &&
		giteeAttachmentPath.MatchString(previous.Path) &&
		path.Base(next.Path) == path.Base(first.Path) && path.Base(next.Path) == path.Base(previous.Path)
}

func (u *updater) snapshot() UpdateInfo { u.mu.Lock(); defer u.mu.Unlock(); return u.info }

func (u *updater) action(w http.ResponseWriter, r *http.Request, action string) {
	// Downloads and executable installation are only controlled by the local app.
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	origin := r.Header.Get("Origin")
	if net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || origin != "http://"+r.Host {
		writeJSON(w, 403, map[string]string{"error": "更新操作仅允许本机应用页面"})
		return
	}
	var e error
	switch action {
	case "start":
		e = u.prepare(true)
	case "prepare":
		e = u.prepare()
	case "cancel":
		u.mu.Lock()
		if u.downloadCancel != nil {
			u.downloadCancel()
		}
		u.mu.Unlock()
	case "install":
		e = u.apply(r.Context())
	}
	if e != nil {
		writeJSON(w, 409, map[string]string{"error": e.Error()})
		return
	}
	writeJSON(w, 200, u.snapshot())
}

func (u *updater) prepare(automatic ...bool) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return errors.New("应用正在退出")
	}
	auto := len(automatic) > 0 && automatic[0]
	if u.info.Phase == "waiting" {
		return nil
	}
	if u.info.Phase == "downloading" {
		u.autoInstall = u.autoInstall || auto
		return nil
	}
	if u.info.Phase == "ready" {
		if auto {
			u.startInstallationLocked()
		}
		return nil
	}
	if u.info.Phase == "installing" {
		return errors.New("正在安装更新")
	}
	if !u.info.CanInstall {
		return errors.New("当前运行方式请下载 Gitee 安装包后更新")
	}
	if !u.info.Available || u.info.DownloadURL == "" {
		return errors.New("Gitee 发行版尚未提供当前系统的安装包")
	}
	if len(u.info.parts) == 0 && !allowedUpdateURL(u.info.DownloadURL) {
		return errors.New("安装包下载地址不受信任")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	u.downloadCancel = cancel
	u.autoInstall = auto
	u.installGeneration++
	u.info.Phase = "downloading"
	u.info.Error = ""
	u.info.DownloadedBytes = 0
	target := u.info
	u.downloads.Add(1)
	go u.download(ctx, target)
	return nil
}

func (u *updater) download(ctx context.Context, target UpdateInfo) {
	defer u.downloads.Done()
	var file, partial string
	var outcome error
	defer func() {
		if partial != "" {
			_ = os.Remove(partial)
		}
		u.mu.Lock()
		defer u.mu.Unlock()
		if ctx.Err() != nil {
			outcome = ctx.Err()
		}
		if u.downloadCancel != nil {
			u.downloadCancel()
			u.downloadCancel = nil
		}
		if outcome != nil {
			if file != "" {
				_ = os.Remove(file)
			}
			u.info.Phase = "error"
			u.info.Error = "下载更新失败，请重试：" + outcome.Error()
			if errors.Is(outcome, context.Canceled) {
				u.info.Phase = "idle"
				u.info.Error = "下载已取消"
			}
			return
		}
		u.prepared = file
		u.preparedVersion = target.Latest
		u.info.Phase = "ready"
		if u.autoInstall && !u.closed {
			u.startInstallationLocked()
		}
	}()
	cache := u.cacheDir
	if cache == "" {
		var base string
		base, outcome = os.UserCacheDir()
		if outcome != nil {
			return
		}
		cache = filepath.Join(base, "learn-with-ai", "updates")
	}
	if outcome = os.MkdirAll(cache, 0700); outcome != nil {
		return
	}
	// A unique private subdirectory prevents simultaneous app instances sharing partial files.
	var dir string
	dir, outcome = os.MkdirTemp(cache, "download-")
	if outcome != nil {
		return
	}
	partial = filepath.Join(dir, "update.msi.partial")
	file = filepath.Join(dir, "update.msi")
	if target.TotalBytes > maxUpdateBytes {
		outcome = errors.New("安装包超过下载上限")
		return
	}
	f, e := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		outcome = e
		return
	}
	hash := sha256.New()
	var written int64
	parts := target.parts
	if len(parts) == 0 {
		parts = []releaseAsset{{URL: target.DownloadURL, Size: target.TotalBytes}}
	}
	for _, part := range parts {
		if outcome = u.downloadPart(ctx, part, io.MultiWriter(f, hash), &written); outcome != nil {
			break
		}
	}
	if e = f.Close(); outcome == nil {
		outcome = e
	}
	if outcome != nil {
		return
	}
	if target.TotalBytes > 0 && target.TotalBytes != written {
		outcome = errors.New("安装包下载不完整")
		return
	}
	if outcome = u.validate(partial, target.Latest); outcome != nil {
		return
	}
	if outcome = os.Rename(partial, file); outcome != nil {
		return
	}
	u.mu.Lock()
	u.preparedHash = hex.EncodeToString(hash.Sum(nil))
	u.mu.Unlock()
}

func (u *updater) downloadPart(ctx context.Context, part releaseAsset, dst io.Writer, written *int64) error {
	if !allowedUpdateURL(part.URL) {
		return errors.New("下载分片地址不受信任")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, part.URL, nil)
	if e != nil {
		return e
	}
	resp, e := u.downloadClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Gitee 返回 %d", resp.StatusCode)
	}
	total := resp.ContentLength
	if total > maxUpdateBytes || (part.Size > 0 && total > 0 && total != part.Size) {
		return errors.New("安装包大小与发行版不符")
	}
	if part.Size <= 0 && total > 0 {
		u.mu.Lock()
		u.info.TotalBytes = total
		u.mu.Unlock()
	}
	var count int64
	buf := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			count += int64(n)
			*written += int64(n)
			if *written > maxUpdateBytes {
				return errors.New("安装包超过下载上限")
			}
			if _, e = dst.Write(buf[:n]); e != nil {
				return e
			}
			u.mu.Lock()
			u.info.DownloadedBytes = *written
			u.mu.Unlock()
		}
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			break
		}
	}
	if (total > 0 && count != total) || (part.Size > 0 && count != part.Size) {
		return errors.New("安装包分片下载不完整")
	}
	return nil
}

// The download worker or start endpoint owns this continuation, never the page.
// mu is held and the worker is registered before exposing waiting.
func (u *updater) startInstallationLocked() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	u.downloadCancel = cancel
	u.installGeneration++
	generation := u.installGeneration
	u.info.Phase = "waiting"
	u.info.Error = ""
	u.downloads.Add(1)
	go func() {
		defer u.downloads.Done()
		defer cancel()
		_ = u.applyMode(ctx, true)
		u.mu.Lock()
		if u.installGeneration == generation {
			u.downloadCancel = nil
		}
		u.mu.Unlock()
	}()
}

func (u *updater) apply(ctx context.Context) error { return u.applyMode(ctx, false) }

func (u *updater) applyMode(ctx context.Context, automatic bool) error {
	u.mu.Lock()
	if u.closed || (u.info.Phase != "ready" && !(automatic && u.info.Phase == "waiting")) || !u.info.CanInstall || !newerVersion(u.preparedVersion, u.current) {
		u.mu.Unlock()
		return errors.New("请先下载可用的新版本")
	}
	file, expected := u.prepared, u.preparedHash
	if !automatic {
		u.info.Phase = "installing"
	}
	u.mu.Unlock()
	fail := func(e error) error {
		u.mu.Lock()
		u.info.Phase = "ready"
		u.info.Error = e.Error()
		u.mu.Unlock()
		return e
	}
	invalid := func(e error) error {
		u.mu.Lock()
		u.info.Phase = "error"
		u.info.Error = e.Error()
		u.prepared = ""
		u.preparedHash = ""
		u.mu.Unlock()
		return e
	}
	fileInfo, e := os.Lstat(file)
	if e != nil || !fileInfo.Mode().IsRegular() {
		return invalid(errors.New("安装包已失效，请重新下载"))
	}
	data, e := os.ReadFile(file)
	if e != nil {
		return invalid(e)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != expected {
		return invalid(errors.New("安装包已改变，请重新下载"))
	}
	if e = u.validate(file, u.preparedVersion); e != nil {
		return invalid(e)
	}
	timeout := 2 * time.Second
	if automatic {
		timeout = 30 * time.Minute
	}
	wait, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, e := workspacegate.Exclusive(wait)
	if e != nil {
		if ctx.Err() != nil {
			return fail(errors.New("自动更新已取消或等待超时，可重试更新"))
		}
		return fail(errors.New("仍有学习或资料任务运行，请结束后重试更新"))
	}
	if e = ctx.Err(); e != nil {
		release(false)
		return fail(errors.New("自动更新已取消"))
	}
	u.mu.Lock()
	u.info.Phase = "installing"
	u.info.Error = ""
	u.mu.Unlock()
	if e = u.install(file); e != nil {
		release(false)
		return fail(e)
	}
	release(true)
	if u.shutdown != nil {
		go func() { time.Sleep(500 * time.Millisecond); u.shutdown() }()
	}
	return nil
}

// UpdateHandler also exposes checking to the browser-based macOS/Linux launcher.
func UpdateHandler(version string, shutdown func()) (http.Handler, func()) {
	u := newUpdater(version)
	u.shutdown = shutdown
	handler := u.handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if httpserver.IsHTMLPreviewHost(r.Host) {
				http.NotFound(w, r)
				return
			}
			handler.ServeHTTP(w, r)
		}), func() {
			u.mu.Lock()
			u.closed = true
			if u.downloadCancel != nil {
				u.downloadCancel()
			}
			u.mu.Unlock()
			u.downloads.Wait()
		}
}
