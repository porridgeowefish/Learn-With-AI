package httpserver

import (
	"errors"
	"log"
	"net/http"
	"os"
	"slices"

	"github.com/xmz14/lll/backend-go/internal/app/projectservice"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	folderstore "github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
)

// handleGetFolders returns the workspace-global folder layout.
func (s *Server) handleGetFolders(w http.ResponseWriter, r *http.Request) {
	s.projectTreeMu.Lock()
	defer s.projectTreeMu.Unlock()
	store, err := folderstore.NewFolderStore()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	layout, err := s.syncDisciplineMapFolders(store)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, layout)
}

// handlePutFolders replaces the whole folder layout. The client computes the
// new state after each create/rename/assign and sends the full layout;
// the store sanitizes and persists atomically. Tree-style membership (a project
// belongs to at most one folder) is enforced server-side.
func (s *Server) handlePutFolders(w http.ResponseWriter, r *http.Request) {
	var in folderstore.Layout
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	s.projectTreeMu.Lock()
	defer s.projectTreeMu.Unlock()
	store, err := folderstore.NewFolderStore()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := store.Replace(in); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := s.syncDisciplineMapFolders(store)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type folderDeletionResult struct {
	Deleted             bool     `json:"deleted"`
	DeletedProjectSlugs []string `json:"deletedProjectSlugs"`
	Error               string   `json:"error,omitempty"`
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProjectSlugs []string `json:"projectSlugs"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil || in.ProjectSlugs == nil {
		httpx.Error(w, http.StatusBadRequest, "请确认要删除的分组内容。")
		return
	}
	s.projectTreeMu.Lock()
	defer s.projectTreeMu.Unlock()
	store, err := folderstore.NewFolderStore()
	if err != nil {
		log.Printf("open folders for deletion: %v", err)
		httpx.Error(w, http.StatusInternalServerError, "无法读取学习分组，请重试。")
		return
	}
	layout, err := s.syncDisciplineMapFolders(store)
	if err != nil {
		log.Printf("sync folders for deletion: %v", err)
		httpx.Error(w, http.StatusInternalServerError, "无法核对分组内容，请重试。")
		return
	}
	id := r.PathValue("id")
	index := slices.IndexFunc(layout.Folders, func(f folderstore.Folder) bool { return f.ID == id })
	if index < 0 {
		httpx.Error(w, http.StatusNotFound, "分组已不存在。")
		return
	}
	folder := layout.Folders[index]
	members := slices.Clone(folder.SlugOrder)
	if folder.MapProjectSlug != "" {
		members = append(members, folder.MapProjectSlug)
	}
	if !slices.Equal(in.ProjectSlugs, members) {
		httpx.Error(w, http.StatusConflict, "分组内容已变化，请重新确认。")
		return
	}
	result := folderDeletionResult{DeletedProjectSlugs: []string{}}
	for _, slug := range members {
		err := s.deleteProject(slug)
		var cleanup *projectservice.FolderCleanupError
		switch {
		case err == nil:
			result.DeletedProjectSlugs = append(result.DeletedProjectSlugs, slug)
			continue
		case errors.As(err, &cleanup):
			result.DeletedProjectSlugs = append(result.DeletedProjectSlugs, slug)
		case errors.Is(err, os.ErrNotExist):
			// A stale assignment is safe to clear only if its whole project root is gone.
			root, rootErr := folderstore.ProjectRootForSlug(slug)
			if rootErr == nil {
				if _, statErr := os.Lstat(root); errors.Is(statErr, os.ErrNotExist) {
					if _, err = store.RemoveProject(slug); err == nil {
						result.DeletedProjectSlugs = append(result.DeletedProjectSlugs, slug)
						continue
					}
				}
			}
		}
		log.Printf("delete folder %s, project %s: %v", id, slug, err)
		result.Error = "无法完成项目「" + slug + "」的删除。请检查项目状态后重试。"
		if errors.Is(err, teacher.ErrResponseActive) {
			result.Error = "项目「" + slug + "」正在生成内容，请等任务结束后再删除分组。"
		}
		httpx.WriteJSON(w, http.StatusOK, result)
		return
	}
	if err := store.RemoveEmptyFolder(id); err != nil {
		log.Printf("delete folder %s: %v", id, err)
		result.Error = "项目已删除，分组记录删除失败。请重试。"
	} else {
		result.Deleted = true
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) syncDisciplineMapFolders(store *folderstore.FolderStore) (folderstore.Layout, error) {
	if err := s.cache.Rebuild(); err != nil {
		return folderstore.Layout{}, err
	}
	maps := make([]folderstore.MapFolderSpec, 0)
	for _, project := range s.cache.All() {
		if project.ProjectType == folderstore.ProjectTypeDisciplineMap {
			maps = append(maps, folderstore.MapFolderSpec{Slug: project.Slug, Title: project.Title})
		}
	}
	return store.SyncMapFolders(maps)
}
