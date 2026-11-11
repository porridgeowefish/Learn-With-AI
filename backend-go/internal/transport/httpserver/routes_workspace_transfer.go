package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/xmz14/lll/backend-go/internal/app/workspaceservice"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"github.com/xmz14/lll/backend-go/internal/platform/workspacegate"
)

// PublishTransferEndpoint lets the installed import script find a private desktop port.
func PublishTransferEndpoint(address string) func() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return func() {}
	}
	dir = filepath.Join(dir, "Learn-With-AI")
	if paths.BuildMode != "release" {
		dir = filepath.Join(dir, "development")
	}
	path := filepath.Join(dir, "transfer-endpoint.json")
	id, err := paths.NewWorkspaceID()
	if err != nil {
		return func() {}
	}
	data, err := json.Marshal(map[string]string{"url": address, "instance": id})
	if err != nil {
		return func() {}
	}
	lockDir := filepath.Join(filepath.Dir(path), ".transfer-discovery")
	guard, err := paths.AcquireWorkspaceInstance(lockDir)
	if err != nil {
		return func() {}
	}
	err = paths.AtomicWriteFile(path, data, 0600)
	guard.Close()
	if err != nil {
		return func() {}
	}
	return func() {
		guard, err := paths.AcquireWorkspaceInstance(lockDir)
		if err != nil {
			return
		}
		defer guard.Close()
		observed, err := os.ReadFile(path)
		if err == nil && string(observed) == string(data) {
			_ = os.Remove(path)
		}
	}
}

func allowTransfer(w http.ResponseWriter, r *http.Request) bool {
	if !allowSecretSettings(w, r) {
		return false
	}
	if transferHeader(r, "Workspace") != "1" {
		httpx.Error(w, 403, "缺少本地工作区请求标记")
		return false
	}
	return true
}

func transferHeader(r *http.Request, name string) string {
	if value := r.Header.Get("X-LWA-" + name); value != "" {
		return value
	}
	return r.Header.Get("X-LLL-" + name)
}

func (s *Server) transferExport(w http.ResponseWriter, r *http.Request) {
	if !allowTransfer(w, r) {
		return
	}
	f, err := os.CreateTemp("", "lwa-export-*.zip")
	if err != nil {
		httpx.Error(w, 500, "无法创建导出文件")
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	release, err := workspacegate.Exclusive(r.Context())
	if err != nil {
		httpx.Error(w, 409, err.Error())
		return
	}
	func() { defer release(false); err = workspaceservice.Export(r.Context(), paths.WORKSPACE, f) }()
	if err != nil {
		httpx.Error(w, 422, err.Error())
		return
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		httpx.Error(w, 500, "无法读取导出文件")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", "attachment; filename=lwa-learning-records.zip")
	w.Header().Set("Content-Type", "application/zip")
	http.ServeContent(w, r, "lwa-learning-records.zip", time.Time{}, f)
}

func (s *Server) transferImport(w http.ResponseWriter, r *http.Request) {
	if !allowTransfer(w, r) {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if id := transferHeader(r, "Operation"); id != "" {
		if len(id) > 96 {
			httpx.Error(w, 400, "导入编号过长")
			return
		}
		if _, busy := s.transferCancellations.LoadOrStore(id, cancel); busy {
			httpx.Error(w, 409, "导入编号已在使用中")
			return
		}
		defer s.transferCancellations.Delete(id)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<30)
	uploadBody := r.Body
	stop := context.AfterFunc(ctx, func() { _ = uploadBody.Close() })
	defer stop()
	f, err := os.CreateTemp("", "lll-import-*.zip")
	if err != nil {
		httpx.Error(w, 500, "无法暂存压缩包")
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()

	_, err = io.Copy(f, r.Body)
	closeErr := f.Close()
	if ctx.Err() != nil {
		httpx.WriteJSON(w, 409, map[string]bool{"cancelled": true})
		return
	}
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			httpx.Error(w, 413, "压缩包超过大小限制")
		} else {
			httpx.Error(w, 400, "无法接收压缩包")
		}
		return
	}
	if closeErr != nil {
		httpx.Error(w, 500, "压缩包暂存失败")
		return
	}
	release, err := workspacegate.Exclusive(ctx)
	if err != nil {
		if ctx.Err() != nil {
			httpx.WriteJSON(w, 409, map[string]bool{"cancelled": true})
			return
		}
		httpx.Error(w, 409, err.Error())
		return
	}
	retire := false
	defer func() { release(retire) }()
	conflicts, err := workspaceservice.Import(ctx, paths.WORKSPACE, f.Name())
	if err != nil {
		if errors.Is(err, workspaceservice.ErrImportRecovery) {
			retire = true
			httpx.Error(w, 500, err.Error())
			s.stopAfterTransfer()
			return
		}
		if err == context.Canceled {
			httpx.WriteJSON(w, 409, map[string]bool{"cancelled": true})
			return
		}
		httpx.Error(w, 422, err.Error())
		return
	}
	if len(conflicts) > 0 {
		httpx.WriteJSON(w, 409, map[string]any{"conflicts": conflicts})
		return
	}
	s.cache.InvalidateAll()
	httpx.WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) transferMove(w http.ResponseWriter, r *http.Request) {
	if !allowTransfer(w, r) {
		return
	}
	var req struct {
		Parent string `json:"parent"`
	}
	if httpx.ReadJSON(r, &req) != nil || !filepath.IsAbs(req.Parent) {
		httpx.Error(w, 400, "请选择绝对路径的父文件夹")
		return
	}
	release, err := workspacegate.Exclusive(r.Context())
	if err != nil {
		httpx.Error(w, 409, err.Error())
		return
	}
	committed := false
	defer func() { release(committed) }()
	target, err := workspaceservice.Move(r.Context(), paths.WORKSPACE, req.Parent)
	if err != nil {
		httpx.Error(w, 422, err.Error())
		return
	}
	committed = true
	httpx.WriteJSON(w, 200, map[string]string{"path": target})
	s.stopAfterTransfer()
}

func (s *Server) stopAfterTransfer() {
	s.mu.RLock()
	stop := s.shutdown
	s.mu.RUnlock()
	if stop != nil {
		time.AfterFunc(time.Second, stop)
	}
}

func (s *Server) cancelTransferImport(w http.ResponseWriter, r *http.Request) {
	if !allowTransfer(w, r) {
		return
	}
	id := transferHeader(r, "Operation")
	v, ok := s.transferCancellations.Load(id)
	if !ok {
		httpx.Error(w, 404, "没有进行中的对应导入")
		return
	}
	v.(context.CancelFunc)()
	httpx.WriteJSON(w, 202, map[string]bool{"requested": true})
}
