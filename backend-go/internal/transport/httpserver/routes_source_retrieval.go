package httpserver

import (
	"net/http"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	sources "github.com/xmz14/lll/backend-go/internal/modules/sources"
)

func (s *Server) handleSourceRetrievalStatus(w http.ResponseWriter, r *http.Request) {
	store, err := sources.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	source, revision, err := store.Get(r.PathValue("sourceId"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	if source.Status == "deleted" || source.Status == "tombstoned" {
		httpx.WriteJSON(w, http.StatusGone, map[string]any{"error": "资料已删除。", "code": "source_deleted"})
		return
	}
	status := source.Retrieval
	if status == nil {
		status = &sources.RetrievalStatus{State: "absent"}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sourceId": source.SourceID, "revisionId": revision.RevisionID, "retrieval": status})
}
