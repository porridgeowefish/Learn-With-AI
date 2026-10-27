package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	assetstore "github.com/xmz14/lll/backend-go/internal/modules/assets"

	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
	sourcestore "github.com/xmz14/lll/backend-go/internal/modules/sources"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
)

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	store, err := teacher.NewConversation(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	if err := teacher.EnsureGreeting(store, r.PathValue("id")); err != nil {
		learningWorkspaceError(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	query := r.URL.Query()
	var projection teacher.Projection
	if _, backward := query["beforeSeq"]; backward {
		before, _ := strconv.ParseUint(query.Get("beforeSeq"), 10, 64)
		projection, err = store.ReadRecent(before, limit)
	} else {
		after, _ := strconv.ParseUint(query.Get("afterSeq"), 10, 64)
		projection, err = store.Read(after, limit)
	}
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projection)
}

func (s *Server) handleExportConversation(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("id")
	store, err := teacher.NewConversation(slug)
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	messages, err := store.AllMessages()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	sourceNames := map[string]string{}
	if sources, sourceErr := sourcestore.New(slug); sourceErr == nil {
		if listed, listErr := sources.List(); listErr == nil {
			for _, source := range listed {
				sourceNames[source.SourceID] = source.DisplayName
			}
		}
	}
	state, stateErr := workspace.ReadProjectState(slug)
	title := slug
	if stateErr == nil {
		title = state.Title
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s — 教师对话导出\n\n", title)
	fmt.Fprintf(&out, "> 导出时间：%s\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	for _, message := range messages {
		role := "教师"
		if message.Role == "learner" {
			role = "学习者"
		}
		fmt.Fprintf(&out, "## %s（%s）\n\n", role, message.CreatedAt.UTC().Format("2006-01-02 15:04"))
		for _, block := range message.Blocks {
			switch block.Type {
			case "markdown":
				out.WriteString(block.Source)
				out.WriteString("\n\n")
			case "reasoning-summary":
				// Reasoning summaries are internal teaching context, not transcript.
			case "attachment":
				name := sourceNames[block.ArtifactRef]
				if name == "" {
					name = block.ArtifactRef
				}
				fmt.Fprintf(&out, "- 附件：%s\n", name)
			}
		}
		out.WriteString("\n")
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-conversation.md"`, slug))
	_, _ = w.Write([]byte(out.String()))
}

func writeQueueError(w http.ResponseWriter, err error) {
	if errors.Is(err, teacher.ErrQueueItemNotFound) {
		httpx.Error(w, http.StatusNotFound, "queued turn not found")
		return
	}
	learningWorkspaceError(w, err)
}

func (s *Server) handleActiveTeacherResponse(w http.ResponseWriter, r *http.Request) {
	run := s.teachingLifecycle().Responses.Project(r.PathValue("id"))
	if run == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	streamTeacherRun(w, r, run)
}

func streamTeacherRun(w http.ResponseWriter, r *http.Request, run *teacher.ActiveResponse) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	after, _ := strconv.Atoi(r.URL.Query().Get("afterFrame"))
	frameID := run.ReplayCursor(r.URL.Query().Get("responseId"), after)
	for {
		frames, done, notify := run.Snapshot(frameID)
		for _, frame := range frames {
			frameID++
			payload, err := json.Marshal(frame.Data)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", frameID, frame.Type, payload); err != nil {
				return
			}
			flusher.Flush()
		}
		if done {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-notify:
		}
	}
}

func (s *Server) handleListLearningAssets(w http.ResponseWriter, r *http.Request) {
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	assets, err := store.List()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"assets": assets})
}

func (s *Server) handleGetLearningAsset(w http.ResponseWriter, r *http.Request) {
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	asset, err := store.Get(r.PathValue("assetKey"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, asset)
}

func (s *Server) handlePutLearningAsset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseEditRevision uint64 `json:"baseEditRevision"`
		Content          string `json:"content"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid asset edit")
		return
	}
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	asset, err := store.UpdateLearner(r.PathValue("assetKey"), in.BaseEditRevision, in.Content)
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	s.broadcaster.Emit("learning-asset-updated", map[string]any{"projectSlug": r.PathValue("id"), "asset": asset.Meta})
	httpx.WriteJSON(w, http.StatusOK, asset)
}

func (s *Server) handleListLearningAssetVersions(w http.ResponseWriter, r *http.Request) {
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	versions, err := store.Versions(r.PathValue("assetKey"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) handlePracticeRounds(w http.ResponseWriter, r *http.Request) {
	store, err := assetstore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	rounds, err := store.PracticeRounds()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"rounds": rounds})
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	store, err := sourcestore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	sources, err := store.List()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	store, err := sourcestore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	source, revision, err := store.Get(r.PathValue("sourceId"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"source": source, "revision": revision})
}

func (s *Server) handleUploadSource(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, sourcestore.DefaultFileLimit+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		httpx.Error(w, http.StatusRequestEntityTooLarge, "source too large or malformed")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if strings.TrimSpace(r.FormValue("operationId")) == "" {
		httpx.Error(w, http.StatusBadRequest, "operationId is required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "one source file is required")
		return
	}
	defer file.Close()
	if header.Size > sourcestore.DefaultFileLimit {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "single source exceeds 50MB")
		return
	}
	if s.sourceProcessor == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "local source processor unavailable")
		return
	}
	store, err := sourcestore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	source, revision, err := store.Add(r.FormValue("displayName"), header.Filename, header.Header.Get("Content-Type"), header.Size, file, false)
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	disposition, reason := sourcestore.ParseDisposition(header.Filename, header.Header.Get("Content-Type"))
	if disposition == "parse" {
		source, err = store.BeginParse(source.SourceID)
	} else {
		source, err = store.SetStatus(source.SourceID, "opaque", reason, "")
	}
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	s.broadcaster.Emit("source-updated", map[string]any{"projectSlug": r.PathValue("id"), "source": source})
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"source": source, "revision": revision, "parseDisposition": disposition, "parseReason": reason})
	if disposition == "parse" {
		s.sourceProcessor.Notify()
	}
}

func (s *Server) handleRetrySource(w http.ResponseWriter, r *http.Request) {
	if s.sourceProcessor == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "local source processor unavailable")
		return
	}
	store, err := sourcestore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	source, _, err := store.Get(r.PathValue("sourceId"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	if source.Status != "failed" && source.Status != "opaque" {
		httpx.Error(w, http.StatusConflict, "source cannot be retried in current state")
		return
	}
	source, err = store.BeginParse(source.SourceID)
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	s.broadcaster.Emit("source-updated", map[string]any{"projectSlug": r.PathValue("id"), "source": source})
	httpx.WriteJSON(w, http.StatusAccepted, source)
	s.sourceProcessor.Notify()
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	store, err := sourcestore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	var source sourcestore.Source
	mutate := func() error {
		if e := sourcestore.CancelSourceStructureJobs(r.PathValue("id"), r.PathValue("sourceId")); e != nil {
			return e
		}
		var e error
		source, e = store.Tombstone(r.PathValue("sourceId"))
		return e
	}
	if s.generation != nil {
		err = s.generation.SourceMutation(mutate)
	} else {
		err = mutate()
	}
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, source)
}

func (s *Server) handlePermanentDeleteSource(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirmation string `json:"confirmation"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil || in.Confirmation != "PERMANENT_DELETE" {
		httpx.Error(w, http.StatusBadRequest, "permanent deletion confirmation is required")
		return
	}
	store, err := sourcestore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	var source sourcestore.Source
	mutate := func() error {
		if e := sourcestore.CancelSourceStructureJobs(r.PathValue("id"), r.PathValue("sourceId")); e != nil {
			return e
		}
		var e error
		source, e = store.PermanentlyDelete(r.PathValue("sourceId"), false)
		return e
	}
	if s.generation != nil {
		err = s.generation.SourceMutation(mutate)
	} else {
		err = mutate()
	}
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	s.broadcaster.Emit("source-updated", map[string]any{"projectSlug": r.PathValue("id"), "source": source})
	httpx.WriteJSON(w, http.StatusOK, source)
}

func (s *Server) handleReadSourceFile(w http.ResponseWriter, r *http.Request) {
	store, err := sourcestore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	source, revision, err := store.Get(r.PathValue("sourceId"))
	if err != nil || revision.RevisionID != r.PathValue("revisionId") || source.Status == "deleted" {
		http.NotFound(w, r)
		return
	}
	fileKey := r.PathValue("fileKey")
	var relative, mediaType string
	if fileKey == "original" {
		relative, mediaType = revision.Original.Path, revision.Original.MediaType
	} else {
		for _, file := range revision.DerivedFiles {
			if file.Key == fileKey {
				relative, mediaType = file.Path, file.MediaType
				break
			}
		}
	}
	if relative == "" {
		http.NotFound(w, r)
		return
	}
	root, _ := workspace.ProjectRootForSlug(r.PathValue("id"))
	base := filepath.Join(root, "sources", source.SourceID, "revisions", revision.RevisionID)
	path, ok := safeWorkspaceFile(base, relative)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	if !strings.HasPrefix(mediaType, "text/") && !strings.HasPrefix(mediaType, "image/") && mediaType != "application/pdf" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	}
	http.ServeFile(w, r, path)
}

// handleReadSourceContent exposes the one parsed Markdown representation used
// for teacher citations. Original files and arbitrary derived files stay out of
// this contract so the picker has one stable, safe-to-preview payload.
func (s *Server) handleReadSourceContent(w http.ResponseWriter, r *http.Request) {
	store, err := sourcestore.New(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	source, revision, err := store.Get(r.PathValue("sourceId"))
	if err != nil || source.Status != "ready" || revision.RevisionID != r.PathValue("revisionId") {
		http.NotFound(w, r)
		return
	}
	var content sourcestore.FileRef
	for _, file := range revision.DerivedFiles {
		if file.Key == "content" {
			content = file
			break
		}
	}
	if content.Key != "content" || !strings.HasPrefix(strings.ToLower(content.MediaType), "text/markdown") {
		http.NotFound(w, r)
		return
	}
	_, _, body, err := store.ReadRevision(source.SourceID, revision.RevisionID, content.SHA256)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write(body)
}

func safeWorkspaceFile(base, relative string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", false
	}
	path := filepath.Join(base, clean)
	back, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(back, "..") {
		return "", false
	}
	info, err := os.Stat(path)
	return path, err == nil && info.Mode().IsRegular()
}

func learningWorkspaceError(w http.ResponseWriter, err error) {
	if errors.Is(err, workspace.ErrLegacyFormat) || errors.Is(err, workspace.ErrInvalidLayout) {
		code, message := "project-layout-invalid", "项目数据不完整或相互矛盾，请手动处理磁盘原件。"
		if errors.Is(err, workspace.ErrLegacyFormat) {
			code, message = "legacy-format-unsupported", "已停止支持旧版专用格式；磁盘原件已保留，请手动处理。"
		}
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{"code": code, "message": message}})
		return
	}
	status := http.StatusInternalServerError
	message := "learning workspace operation failed"
	if errors.Is(err, context.Canceled) {
		status, message = 499, "request cancelled"
	}
	if errors.Is(err, http.ErrMissingFile) || errors.Is(err, os.ErrNotExist) {
		status, message = http.StatusNotFound, "project or record not found"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		status, message = http.StatusGatewayTimeout, "operation timed out"
	}
	var conflict *assetstore.ConflictError
	if errors.As(err, &conflict) {
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{"error": map[string]any{"code": "asset_edit_conflict", "message": "资产已被其他编辑更新", "currentEditRevision": conflict.CurrentRevision}})
		return
	}
	if strings.Contains(strings.ToLower(err.Error()), "not exist") {
		status, message = http.StatusNotFound, "record not found"
	}
	if strings.Contains(err.Error(), "limit") || strings.Contains(err.Error(), "byte count") {
		status, message = http.StatusRequestEntityTooLarge, "source exceeds configured limits"
	}
	if strings.Contains(err.Error(), "active task") {
		status, message = http.StatusConflict, "source revision is in use"
	}
	httpx.WriteJSON(w, status, map[string]any{"error": map[string]any{"code": "learning_workspace_error", "message": message}})
}
