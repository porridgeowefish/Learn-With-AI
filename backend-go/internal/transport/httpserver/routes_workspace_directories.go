package httpserver

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/xmz14/lll/backend-go/internal/app/workspaceservice"
	"github.com/xmz14/lll/backend-go/internal/httpx"
)

func workspaceDirectories(w http.ResponseWriter, r *http.Request) {
	if !allowTransfer(w, r) {
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "无法读取目录路径")
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
