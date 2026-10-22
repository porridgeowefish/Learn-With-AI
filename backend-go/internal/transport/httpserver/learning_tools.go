package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/activity"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/assets"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"net/http"
)

func (s *Server) learningTools() *teacher.LearningTools {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tools == nil {
		s.tools = teacher.NewLearningTools()
		s.tools.OnActivity = func(slug string, e activity.ProgressEvent) { _, _, _ = s.recordLearningActivity(slug, e) }
		s.tools.OnAnnotationUpdated = func(slug, action, id string) { s.emitAnnotationUpdated(slug, action, id) }
	}
	return s.tools
}
func writeToolError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, teacher.ErrReviewEmpty):
		httpx.WriteJSON(w, 409, map[string]any{"error": map[string]any{"code": "review_empty", "message": "复习稿还是空的，先生成或编写复习稿，再来讲给教师听。"}})
	case errors.Is(err, teacher.ErrAIUnavailable), errors.Is(err, teacher.ErrProviderMissing):
		httpx.Error(w, 400, err.Error())
	case errors.Is(err, teacher.ErrAnnotationMissing), errors.Is(err, assets.ErrFeynmanSessionNotFound):
		httpx.Error(w, 404, err.Error())
	default:
		writeTeacherError(w, err)
	}
}
func streamTool(w http.ResponseWriter, r *http.Request, run teacher.ToolStream) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, 500, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	emit := func(f ai.AIFrame) { b, _ := json.Marshal(f); fmt.Fprintf(w, "data: %s\n\n", b); flusher.Flush() }
	if err := run(r.Context(), emit); err != nil {
		emit(ai.AIFrame{Type: "error", Content: "回答暂时中断，已保留收到的部分内容。"})
	}
}
func (s *Server) handleAskAiStream(w http.ResponseWriter, r *http.Request) {
	var in teacher.ToolInput
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "invalid body")
		return
	}
	run, err := s.learningTools().PrepareAnnotationAsk(r.PathValue("id"), r.PathValue("annotationId"), in)
	if err != nil {
		writeToolError(w, err)
		return
	}
	streamTool(w, r, run)
}
func (s *Server) handleAskAiSummarize(w http.ResponseWriter, r *http.Request) {
	if err := s.learningTools().SummarizeAnnotation(r.PathValue("id"), r.PathValue("annotationId")); err != nil {
		writeToolError(w, err)
		return
	}
	httpx.WriteJSON(w, 202, map[string]any{"status": "pending"})
}
func (s *Server) handleCreateFeynmanSession(w http.ResponseWriter, r *http.Request) {
	session, err := teacher.CreateFeynmanSession(r.PathValue("id"))
	if err != nil {
		writeToolError(w, err)
		return
	}
	httpx.WriteJSON(w, 201, session)
}
func (s *Server) handleFeynmanStream(w http.ResponseWriter, r *http.Request) {
	var in teacher.ToolInput
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "invalid body")
		return
	}
	run, err := s.learningTools().PrepareFeynman(r.PathValue("id"), r.PathValue("sessionId"), in)
	if err != nil {
		writeToolError(w, err)
		return
	}
	streamTool(w, r, run)
}
