package httpserver

import (
	"net/http"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	folderstore "github.com/xmz14/lll/backend-go/internal/modules/projects"
)

// handleGetFolders returns the workspace-global folder layout.
func (s *Server) handleGetFolders(w http.ResponseWriter, r *http.Request) {
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
// new state after each create/rename/delete/assign and sends the full layout;
// the store sanitizes and persists atomically. Tree-style membership (a project
// belongs to at most one folder) is enforced server-side.
func (s *Server) handlePutFolders(w http.ResponseWriter, r *http.Request) {
	var in folderstore.Layout
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
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
