package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

func (s *Server) getLearningPointRules(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminAuthorize(w, r, true); !ok {
		return
	}
	rules, err := s.db.ActiveLearningPointRules(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "learning_point_rules_unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, http.StatusOK, rules)
}

func (s *Server) saveLearningPointRules(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminAuthorize(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Version            *int64 `json:"version"`
		PracticePoints     *int64 `json:"practicePoints"`
		ConversationPoints *int64 `json:"conversationPoints"`
	}
	if !decode(w, r, &in, 2048) {
		return
	}
	if in.Version == nil || in.PracticePoints == nil || in.ConversationPoints == nil {
		fail(w, http.StatusBadRequest, "invalid_learning_point_rules")
		return
	}
	rules, err := s.db.SaveLearningPointRules(r.Context(), *in.Version, admin.ID, store.LearningPointValues{
		PracticePoints: *in.PracticePoints, ConversationPoints: *in.ConversationPoints,
	}, time.Now())
	switch {
	case errors.Is(err, store.ErrConflict):
		fail(w, http.StatusConflict, "learning_point_rules_changed")
	case errors.Is(err, store.ErrInvalidLearningPointRules):
		fail(w, http.StatusBadRequest, "invalid_learning_point_rules")
	case err != nil:
		fail(w, http.StatusInternalServerError, "learning_point_rules_unavailable")
	default:
		respond(w, http.StatusOK, rules)
	}
}
