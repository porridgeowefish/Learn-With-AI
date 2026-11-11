package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

var errRatingSync = errors.New("teaching rating sync failed")

func (s *Server) handleLessonRating(w http.ResponseWriter, r *http.Request) {
	var input struct {
		MessageID string
		Stars     *int
	}
	if err := httpx.ReadJSON(r, &input); err != nil || input.MessageID == "" || input.Stars == nil || *input.Stars < 0 || *input.Stars > 5 {
		httpx.Error(w, http.StatusBadRequest, "invalid_lesson_rating")
		return
	}
	conversation, err := teacher.NewConversation(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	message, exists, err := conversation.FindMessage(input.MessageID)
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	if !exists || message.Role != "teacher" || message.Status != "completed" || !message.LessonEnded {
		httpx.Error(w, http.StatusNotFound, "lesson_end_not_found")
		return
	}
	submit := func() error {
		if *input.Stars == 0 || message.LessonRatingExposureID == "" {
			return nil
		}
		client, err := llmcloud.Default()
		if err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			defer cancel()
			err = client.RateTeaching(ctx, message.LessonRatingExposureID, *input.Stars)
		}
		var remote *llmcloud.RemoteError
		// Old server releases and sessions without the original exposure
		// can still save the score on this device.
		if err != nil && !errors.Is(err, llmcloud.ErrLoginRequired) &&
			!(errors.As(err, &remote) && remote.Status == http.StatusNotFound) {
			return errRatingSync
		}
		return nil
	}
	if err := conversation.SaveLessonRating(message.LessonRatingKey(), *input.Stars, submit); errors.Is(err, teacher.ErrLessonAlreadyRated) {
		httpx.Error(w, http.StatusConflict, "lesson_already_rated")
		return
	} else if errors.Is(err, errRatingSync) {
		httpx.Error(w, http.StatusServiceUnavailable, "lesson_rating_unavailable")
		return
	} else if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
