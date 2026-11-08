package httpserver

import (
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"net/http"
)

func (s *Server) teachingSummaries(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("id")
	if _, err := teacher.NewConversation(slug); err != nil {
		learningWorkspaceError(w, err)
		return
	}
	if r.Method == http.MethodPost {
		if s.summaries == nil {
			httpx.Error(w, 503, "summary service unavailable")
			return
		}
		var err error
		if r.PathValue("action") == "retry" {
			err = s.summaries.Retry(slug, r.PathValue("summaryId"))
		} else if r.PathValue("action") == "revoke" {
			err = teacher.RevokeTeachingSummary(slug, r.PathValue("summaryId"))
		} else {
			httpx.Error(w, 404, "unknown summary action")
			return
		}
		if err != nil {
			httpx.Error(w, 409, err.Error())
			return
		}
	}
	items, err := teacher.TeachingSummaries(slug)
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"summaries": items})
}
