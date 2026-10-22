package httpserver

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	progressstore "github.com/xmz14/lll/backend-go/internal/modules/activity"
	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
)

func (s *Server) handleGetActivity(w http.ResponseWriter, r *http.Request) {
	weeks := 26
	if raw := r.URL.Query().Get("weeks"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "weeks must be 26 or 52")
			return
		}
		weeks = parsed
	}
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	if project != "" && !workspace.ValidateSlug(project) {
		httpx.Error(w, http.StatusBadRequest, "invalid project")
		return
	}
	summary, err := progressstore.Aggregate(project, weeks, time.Now())
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"summary": summary})
}

func (s *Server) handlePostActivity(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("id")
	if !workspace.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug")
		return
	}
	var in struct {
		ID            string `json:"id"`
		SourceID      string `json:"sourceId"`
		Title         string `json:"title"`
		Detail        string `json:"detail"`
		ActivityDelta int    `json:"activityDelta"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	in.ID, in.SourceID, in.Title, in.Detail = strings.TrimSpace(in.ID), strings.TrimSpace(in.SourceID), strings.TrimSpace(in.Title), strings.TrimSpace(in.Detail)
	if in.ID == "" || len(in.ID) > 180 || in.SourceID == "" || len(in.SourceID) > 180 || len(in.Title) > 100 || len(in.Detail) > 240 || in.ActivityDelta < 1 || in.ActivityDelta > 5 {
		httpx.Error(w, http.StatusBadRequest, "invalid activity event")
		return
	}
	event, added, err := s.recordLearningActivity(slug, progressstore.ProgressEvent{
		ID: "reading:" + in.ID, SourceType: "reading", SourceID: in.SourceID,
		ActivityDelta: in.ActivityDelta, Title: in.Title, Detail: in.Detail,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"event": event, "added": added})
}

// recordLearningActivity is the transport-side bridge from the durable
// progress stream to the single application SSE connection. A duplicate event
// is deliberately silent: the persisted ID is the idempotency boundary.
func (s *Server) recordLearningActivity(slug string, event progressstore.ProgressEvent) (progressstore.ProgressEvent, bool, error) {
	awarded, added, err := progressstore.RecordActivity(slug, event)
	if err == nil && added && s.broadcaster != nil {
		s.broadcaster.Emit("learning-activity-updated", map[string]any{"projectSlug": slug})
	}
	return awarded, added, err
}
