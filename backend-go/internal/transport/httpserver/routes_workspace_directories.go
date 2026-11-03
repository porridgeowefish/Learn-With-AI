package httpserver

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/xmz14/lll/backend-go/internal/app/workspaceservice"
	"github.com/xmz14/lll/backend-go/internal/httpx"
)

func workspaceDirectories(w http.ResponseWriter, r *http.Request) {
	serveWorkspaceDirectories(w, r, pickWorkspaceParent)
}

func serveWorkspaceDirectories(w http.ResponseWriter, r *http.Request, pick func(context.Context) (string, error)) {
	if !allowTransfer(w, r) {
		return
	}
	var in struct {
		Path string `json:"path"`
		Pick bool   `json:"pick"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "无法读取目录路径")
		return
	}
	if in.Pick {
		path, err := pick(r.Context())
		if err != nil {
			httpx.Error(w, 500, err.Error())
			return
		}
		if path == "" {
			httpx.WriteJSON(w, 200, map[string]bool{"cancelled": true})
			return
		}
		destination, err := workspaceservice.MoveDestination(path)
		if err != nil {
			httpx.Error(w, 400, "无法预览工作区位置")
			return
		}
		httpx.WriteJSON(w, 200, map[string]string{"path": path, "destination": destination})
		return
	}
	if in.Path == "" {
		in.Path, _ = os.UserHomeDir()
	}
	if !filepath.IsAbs(in.Path) {
		httpx.Error(w, 400, "请输入绝对路径")
		return
	}
	dir := filepath.Clean(in.Path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		httpx.Error(w, 400, "无法打开目录")
		return
	}
	children := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			children = append(children, filepath.Join(dir, entry.Name()))
		}
	}
	destination, err := workspaceservice.MoveDestination(dir)
	if err != nil {
		httpx.Error(w, 400, "无法预览工作区位置")
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"destination": destination, "path": dir, "parent": filepath.Dir(dir), "children": children})
}
