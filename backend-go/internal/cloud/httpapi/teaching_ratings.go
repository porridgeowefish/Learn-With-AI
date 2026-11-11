package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

func (s *Server) teachingRating(w http.ResponseWriter, r *http.Request) {
	account, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	var input struct {
		ExposureID string
		Stars      int
	}
	if !decode(w, r, &input, 2048) {
		return
	}
	if input.ExposureID == "" || len(input.ExposureID) > 128 || input.Stars < 1 || input.Stars > 5 {
		fail(w, http.StatusBadRequest, "invalid_teaching_rating")
		return
	}
	switch err := s.db.RateTeaching(r.Context(), account.ID, input.ExposureID, input.Stars, time.Now()); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "teaching_exposure_not_found")
	case errors.Is(err, store.ErrConflict):
		fail(w, http.StatusConflict, "teaching_rating_conflict")
	default:
		fail(w, http.StatusInternalServerError, "teaching_rating_unavailable")
	}
}
