package httpserver

import (
	"errors"
	"fmt"

	"net/http"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	progressstore "github.com/xmz14/lll/backend-go/internal/modules/activity"
	assetstore "github.com/xmz14/lll/backend-go/internal/modules/assets"
)

// ---- practice results: one saved score per practice round ----

func (s *Server) handleListPracticeResults(w http.ResponseWriter, r *http.Request) {
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	results, err := store.PracticeResults()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handlePutPracticeResult(w http.ResponseWriter, r *http.Request) {
	var in assetstore.PracticeResult
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid practice result")
		return
	}
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	saved, err := store.SavePracticeResult(r.PathValue("versionId"), in)
	switch {
	case errors.Is(err, assetstore.ErrInvalidPracticeResult):
		httpx.Error(w, http.StatusBadRequest, "invalid practice result")
		return
	case errors.Is(err, assetstore.ErrPracticeRoundNotFound):
		httpx.Error(w, http.StatusNotFound, "practice round not found")
		return
	case err != nil:
		learningWorkspaceError(w, err)
		return
	}
	_, _, _ = s.recordLearningActivity(r.PathValue("id"), progressstore.ProgressEvent{
		ID: "practice-result:" + saved.VersionID + ":" + saved.SubmittedAt.Format("20060102T150405.000"), SourceType: "practice-result", SourceID: saved.VersionID,
		ActivityDelta: 1, Title: "完成练习测试", Detail: fmt.Sprintf("得分 %d/%d", saved.Score, saved.Total),
	})
	httpx.WriteJSON(w, http.StatusOK, saved)
}

func (s *Server) handleDeletePracticeResult(w http.ResponseWriter, r *http.Request) {
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	if err := store.DeletePracticeResult(r.PathValue("versionId")); err != nil {
		if errors.Is(err, assetstore.ErrPracticeRoundNotFound) {
			httpx.Error(w, http.StatusNotFound, "practice round not found")
			return
		}
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// ---- 讲给教师听 (Feynman mode) ----

func (s *Server) handleListFeynmanSessions(w http.ResponseWriter, r *http.Request) {
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	sessions, err := store.FeynmanSessions()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}
