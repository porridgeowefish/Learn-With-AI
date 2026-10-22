package httpserver

import (
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"net/http"
)

func (s *Server) handleTeacherOpening(w http.ResponseWriter, r *http.Request) {
	var in struct{ ProviderID string }
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "invalid opening request")
		return
	}
	run, err := s.teachingLifecycle().Opening(r.PathValue("id"), in.ProviderID)
	if err != nil {
		writeTeacherError(w, err)
		return
	}
	if run == nil {
		w.WriteHeader(204)
		return
	}
	streamTeacherRun(w, r, run)
}
