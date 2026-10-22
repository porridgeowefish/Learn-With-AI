package httpserver

import (
	"errors"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/generation"
	"net/http"
	"strconv"
)

func generationError(w http.ResponseWriter, err error) {
	code := generation.ErrorCode(err)
	status := generation.ErrorStatus(err)
	body := map[string]any{"error": map[string]any{"code": code, "message": code}}
	var api *generation.APIError
	if errors.As(err, &api) && api.JobID != "" {
		body["jobId"] = api.JobID
	}
	httpx.WriteJSON(w, status, body)
}
func (s *Server) generationService(w http.ResponseWriter) *generation.Service {
	if s.generation == nil {
		httpx.WriteJSON(w, 503, map[string]any{"error": map[string]string{"code": "generation-unavailable"}})
	}
	return s.generation
}
func (s *Server) handleCreateGeneration(w http.ResponseWriter, r *http.Request) {
	g := s.generationService(w)
	if g == nil {
		return
	}
	var in generation.Input
	if httpx.ReadJSON(r, &in) != nil {
		httpx.Error(w, 400, "invalid body")
		return
	}
	j, e := g.Create(r.Context(), r.PathValue("id"), in)
	if e != nil {
		generationError(w, e)
		return
	}
	httpx.WriteJSON(w, 202, map[string]any{"job": j})
}
func (s *Server) handleListGeneration(w http.ResponseWriter, r *http.Request) {
	g := s.generationService(w)
	if g == nil {
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 {
			httpx.Error(w, 400, "invalid limit")
			return
		}
		limit = n
	}
	jobs, next, e := g.List(r.PathValue("id"), limit, r.URL.Query().Get("cursor"))
	if e != nil {
		generationError(w, e)
		return
	}
	body := map[string]any{"jobs": jobs}
	if next != "" {
		body["nextCursor"] = next
	}
	httpx.WriteJSON(w, 200, body)
}
func (s *Server) handleGetGeneration(w http.ResponseWriter, r *http.Request) {
	g := s.generationService(w)
	if g == nil {
		return
	}
	j, e := g.Get(r.PathValue("id"), r.PathValue("jobId"))
	if e != nil {
		generationError(w, e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"job": j})
}
func (s *Server) handleCancelGeneration(w http.ResponseWriter, r *http.Request) {
	g := s.generationService(w)
	if g == nil {
		return
	}
	j, e := g.Cancel(r.PathValue("id"), r.PathValue("jobId"))
	if e != nil {
		generationError(w, e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"job": j})
}
func (s *Server) handleGenerationCandidate(w http.ResponseWriter, r *http.Request) {
	g := s.generationService(w)
	if g == nil {
		return
	}
	candidate, err := g.Candidate(r.PathValue("id"), r.PathValue("jobId"))
	if err != nil {
		generationError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, candidate)
}
func (s *Server) handleRetryGeneration(w http.ResponseWriter, r *http.Request) {
	g := s.generationService(w)
	if g == nil {
		return
	}
	var in struct {
		OperationID string `json:"operationId"`
	}
	if httpx.ReadJSON(r, &in) != nil {
		httpx.Error(w, 400, "invalid body")
		return
	}
	j, e := g.Retry(r.Context(), r.PathValue("id"), r.PathValue("jobId"), in.OperationID)
	if e != nil {
		generationError(w, e)
		return
	}
	httpx.WriteJSON(w, 202, map[string]any{"job": j})
}
func (s *Server) handleDisciplineMap(w http.ResponseWriter, r *http.Request) {
	m, e := generation.ReadMap(r.PathValue("id"), r.URL.Query().Get("versionId"))
	if e != nil {
		generationError(w, e)
		return
	}
	httpx.WriteJSON(w, 200, m)
}
