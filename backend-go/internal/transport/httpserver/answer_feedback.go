package httpserver

import (
	"errors"
	"net/http"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

type answerFeedbackInput struct {
	MessageID string `json:"messageId"`
	Value     string `json:"value"`
}

func (s *Server) handleTeacherFeedback(w http.ResponseWriter, r *http.Request) {
	var input answerFeedbackInput
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_answer_feedback")
		return
	}
	if input.MessageID == "" || (input.Value != "up" && input.Value != "down") {
		httpx.Error(w, http.StatusBadRequest, "invalid_answer_feedback")
		return
	}
	store, err := teacher.NewConversation(r.PathValue("id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "conversation_unavailable")
		return
	}
	writeFeedbackResult(w, store.SaveAnswerFeedback(input.MessageID, input.Value))
}

func (s *Server) handleComparisonVote(w http.ResponseWriter, r *http.Request) {
	var input answerFeedbackInput
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_comparison_vote")
		return
	}
	if input.MessageID == "" || (input.Value != "a" && input.Value != "b" && input.Value != "tie" && input.Value != "skipped") {
		httpx.Error(w, http.StatusBadRequest, "invalid_comparison_vote")
		return
	}
	writeFeedbackResult(w, s.teachingLifecycle().VoteComparison(r.PathValue("id"), input.MessageID, input.Value, func(id string) error {
		client, err := llmcloud.Default()
		if err != nil {
			return err
		}
		return client.VoteComparison(r.Context(), id, input.Value)
	}))
}

func writeFeedbackResult(w http.ResponseWriter, err error) {
	var remote *llmcloud.RemoteError
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, teacher.ErrFeedbackNotFound):
		httpx.Error(w, http.StatusNotFound, "answer_unavailable")
	case errors.Is(err, teacher.ErrComparisonPending):
		httpx.Error(w, http.StatusConflict, "comparison_vote_unavailable")
	case errors.Is(err, llmcloud.ErrLoginRequired), errors.As(err, &remote) && (remote.Status == http.StatusNotFound || remote.Status == http.StatusUnauthorized):
		httpx.Error(w, http.StatusGone, "comparison_identity_unavailable")
	default:
		httpx.Error(w, http.StatusServiceUnavailable, "feedback_unavailable")
	}
}
