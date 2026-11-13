package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
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
	refreshConversationCredits(r.Context(), projection.Messages)
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
	asset, err := store.UpdateLearner("practice", in.BaseEditRevision, in.Content)
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
	const sizeMessage = "single source exceeds 100 MiB"
	r.Body = http.MaxBytesReader(w, r.Body, sourcestore.DefaultFileLimit+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || errors.Is(err, multipart.ErrMessageTooLarge) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, sizeMessage)
		} else {
			httpx.Error(w, http.StatusBadRequest, "malformed source upload")
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	operationID := strings.TrimSpace(r.FormValue("operationId"))
	if operationID == "" || len(operationID) > 128 {
		httpx.Error(w, http.StatusBadRequest, "operationId must be between 1 and 128 bytes")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "one source file is required")
		return
	}
	defer file.Close()
	if header.Size > sourcestore.DefaultFileLimit {
		httpx.Error(w, http.StatusRequestEntityTooLarge, sizeMessage)
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
	source, revision, created, err := store.Upload(operationID, r.FormValue("displayName"), header.Filename, header.Header.Get("Content-Type"), header.Size, file)
	if err != nil {
		if errors.Is(err, sourcestore.ErrUploadConflict) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		learningWorkspaceError(w, err)
		return
	}
	disposition, reason := sourcestore.ParseDisposition(revision.Original.Filename, revision.Original.MediaType)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		s.broadcaster.Emit("source-updated", map[string]any{"projectSlug": r.PathValue("id"), "source": source})
	}
	httpx.WriteJSON(w, status, map[string]any{"source": source, "revision": revision, "parseDisposition": disposition, "parseReason": reason})
	if source.Status == "processing" {
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
