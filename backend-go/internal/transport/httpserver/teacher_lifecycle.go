package httpserver

import (
	"errors"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"net/http"
)

func (s *Server) teachingLifecycle() *teacher.Lifecycle {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifecycle == nil {
		s.lifecycle = teacher.NewLifecycle(s.teacher)
		s.lifecycle.OnQueueUpdated = func(slug string) {
			s.broadcaster.Emit("conversation-queue-updated", map[string]any{"projectSlug": slug})
		}
	}
	return s.lifecycle
}
func writeTeacherError(w http.ResponseWriter, err error) {
	status := 0
	switch {
	case errors.Is(err, teacher.ErrResponseActive), errors.Is(err, teacher.ErrNotLatestResponse):
		status = 409
	case errors.Is(err, teacher.ErrResponseNotFound), errors.Is(err, teacher.ErrQueueItemNotFound):
		status = 404
	case errors.Is(err, teacher.ErrInvalidTurn):
		status = 400
	case errors.Is(err, teacher.ErrStopTimeout), errors.Is(err, teacher.ErrLifecycleClosed):
		status = 503
	}
	if status != 0 {
		httpx.Error(w, status, err.Error())
		return
	}
	learningWorkspaceError(w, err)
}
func (s *Server) handleTeacherTurn(w http.ResponseWriter, r *http.Request) {
	var in teacher.TurnInput
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "invalid teacher turn")
		return
	}
	run, err := s.teachingLifecycle().Turn(r.PathValue("id"), in)
	if err != nil {
		writeTeacherError(w, err)
		return
	}
	streamTeacherRun(w, r, run)
}

func (s *Server) handleQueueTeacherTurn(w http.ResponseWriter, r *http.Request) {
	var in teacher.TurnInput
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "invalid queued turn")
		return
	}
	id, promoted, err := s.teachingLifecycle().Queue(r.PathValue("id"), in)
	if err != nil {
		writeTeacherError(w, err)
		return
	}
	httpx.WriteJSON(w, 202, map[string]any{"queueId": id, "promoted": promoted})
}

func (s *Server) handleEditQueuedTurn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content        string
		AttachmentRefs []string
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "invalid queued turn edit")
		return
	}
	id := r.PathValue("queueId")
	if err := s.teachingLifecycle().EditQueue(r.PathValue("id"), id, in.Content, in.AttachmentRefs); err != nil {
		writeTeacherError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"queueId": id})
}

func (s *Server) handleDiscardQueuedTurn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("queueId")
	if err := s.teachingLifecycle().DiscardQueue(r.PathValue("id"), id); err != nil {
		writeTeacherError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"queueId": id, "discarded": true})
}

func (s *Server) handleSteerQueuedTurn(w http.ResponseWriter, r *http.Request) {
	run, err := s.teachingLifecycle().Steer(r.Context(), r.PathValue("id"), r.PathValue("queueId"))
	if err != nil {
		writeTeacherError(w, err)
		return
	}
	streamTeacherRun(w, r, run)
}

func (s *Server) handleRegenerateTeacherResponse(w http.ResponseWriter, r *http.Request) {
	var in struct{ ProviderID string }
	_ = httpx.ReadJSON(r, &in)
	run, err := s.teachingLifecycle().Regenerate(r.PathValue("id"), r.PathValue("responseId"), in.ProviderID)
	if err != nil {
		writeTeacherError(w, err)
		return
	}
	streamTeacherRun(w, r, run)
}

func (s *Server) handleStopTeacherResponse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("responseId")
	if err := s.teachingLifecycle().Stop(r.PathValue("id"), id); err != nil {
		writeTeacherError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"stopped": true, "responseId": id})
}
