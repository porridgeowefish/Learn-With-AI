package httpapi

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

var replyFeedbackID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Server) submitReplyFeedback(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ReplyID, Model, Value string
		Revision              uint64
	}
	if !decode(w, r, &input, 2048) {
		return
	}
	input.Model = strings.TrimSpace(input.Model)
	if !replyFeedbackID.MatchString(input.ReplyID) || (input.Value != "up" && input.Value != "down") ||
		input.Revision == 0 || input.Revision > 1<<53 || len(input.Model) > 256 || strings.IndexFunc(input.Model, unicode.IsControl) >= 0 {
		fail(w, http.StatusBadRequest, "invalid_reply_feedback")
		return
	}
	if !s.allowAuth(w, r, "reply-feedback", s.clientIP(r), 120) {
		return
	}
	if err := s.db.SaveReplyFeedback(r.Context(), input.ReplyID, input.Model, input.Value, input.Revision, time.Now()); err != nil {
		fail(w, http.StatusServiceUnavailable, "feedback_unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listReplyFeedback(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminAuthorize(w, r, false); !ok {
		return
	}
	value := r.URL.Query().Get("value")
	rawOffset := r.URL.Query().Get("offset")
	offset, err := strconv.Atoi(rawOffset)
	if (rawOffset != "" && err != nil) || offset < 0 || offset > 1_000_000 || (value != "" && value != "up" && value != "down") {
		fail(w, http.StatusBadRequest, "invalid_reply_feedback_query")
		return
	}
	rows, err := s.db.ReplyFeedback(r.Context(), value, offset)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "feedback_unavailable")
		return
	}
	type replyPage struct {
		Rows []store.ReplyFeedback `json:"rows"`
		More bool                  `json:"more"`
	}
	respond(w, http.StatusOK, replyPage{Rows: rows[:min(len(rows), 50)], More: len(rows) > 50})
}
