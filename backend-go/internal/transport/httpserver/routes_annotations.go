package httpserver

import (
	"errors"
	"net/http"
	"os"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	annotationstore "github.com/xmz14/lll/backend-go/internal/modules/assets"
	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
)

func openAnnotationStore(slug string) (*annotationstore.AnnotationStore, error) {
	store, err := annotationstore.NewAnnotations(slug)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Server) handleListAnnotations(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("id")
	if !workspace.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug")
		return
	}
	store, err := openAnnotationStore(slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	annotations, err := store.List(false)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"annotations": annotations})
}

func (s *Server) handleCreateAnnotation(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("id")
	if !workspace.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug")
		return
	}
	var in struct {
		AssetVersionID string                  `json:"assetVersionId"`
		QuoteSnapshot  string                  `json:"quoteSnapshot"`
		Anchors        annotationstore.Anchors `json:"anchors"`
		Note           string                  `json:"note"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	store, err := openAnnotationStore(slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	annotation, err := store.Create(annotationstore.CreateInput{AssetVersionID: in.AssetVersionID, QuoteSnapshot: in.QuoteSnapshot, Anchors: in.Anchors, Note: in.Note})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	s.emitAnnotationUpdated(slug, "create", annotation.AnnotationID)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"annotation": annotation})
}

func (s *Server) handleUpdateAnnotation(w http.ResponseWriter, r *http.Request) {
	slug, id := r.PathValue("id"), r.PathValue("annotationId")
	if !workspace.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug")
		return
	}
	var in struct {
		Note   *string                `json:"note"`
		Status *annotationstore.State `json:"status"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	store, err := openAnnotationStore(slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	annotation, err := store.Update(id, in.Note, in.Status)
	if errors.Is(err, os.ErrNotExist) {
		httpx.Error(w, http.StatusNotFound, "annotation not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	s.emitAnnotationUpdated(slug, "update", id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"annotation": annotation})
}

func (s *Server) handleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	slug, id := r.PathValue("id"), r.PathValue("annotationId")
	if !workspace.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug")
		return
	}
	store, err := openAnnotationStore(slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := store.Delete(id); errors.Is(err, os.ErrNotExist) {
		httpx.Error(w, http.StatusNotFound, "annotation not found")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.emitAnnotationUpdated(slug, "delete", id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAnnotationAskAiStream(w http.ResponseWriter, r *http.Request) {
	s.handleAskAiStream(w, r)
}

func (s *Server) handleAnnotationAskAiSummarize(w http.ResponseWriter, r *http.Request) {
	s.handleAskAiSummarize(w, r)
}

func (s *Server) emitAnnotationUpdated(slug, action, id string) {
	if s.broadcaster != nil {
		payload := map[string]any{"projectSlug": slug, "action": action, "id": id}
		s.broadcaster.Emit("annotation-updated", payload)
	}
}
