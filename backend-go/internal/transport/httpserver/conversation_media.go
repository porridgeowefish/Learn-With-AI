package httpserver

import (
	"context"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

func (s *Server) handleSpeechCapability(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, teacher.LocalSpeechCapability(r.Context()))
}

func (s *Server) handleVoiceTranscription(w http.ResponseWriter, r *http.Request) {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host != r.Host {
		httpx.Error(w, http.StatusForbidden, "请从应用页面使用语音输入")
		return
	}
	if _, err := teacher.NewConversation(r.PathValue("id")); err != nil {
		learningWorkspaceError(w, err)
		return
	}
	// This bounds each transport chunk, not the duration of the recording.
	const maxChunkBytes = 60*32000 + 4096
	r.Body = http.MaxBytesReader(w, r.Body, maxChunkBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "语音分段过大，请重试")
		return
	}
	lock := s.chatMediaLock(r.PathValue("id"))
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	text, err := teacher.TranscribeVoice(ctx, data)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *Server) handleChatAttachment(w http.ResponseWriter, r *http.Request) {
	lock := s.chatMediaLock(r.PathValue("id"))
	lock.Lock()
	defer lock.Unlock()
	if _, err := teacher.NewConversation(r.PathValue("id")); err != nil {
		learningWorkspaceError(w, err)
		return
	}
	// Multipart requests can be sent cross-origin without preflight. Require our own page.
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host != r.Host {
		httpx.Error(w, http.StatusForbidden, "请从应用页面上传聊天附件")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, teacher.MaxChatAttachmentBytes+(1<<20))
	if err := r.ParseMultipartForm(teacher.MaxChatAttachmentBytes + (1 << 20)); err != nil {
		httpx.Error(w, http.StatusBadRequest, "附件过大或上传无效")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "请选择一个图片或音频文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, teacher.MaxChatAttachmentBytes+1))
	if err != nil || len(data) > teacher.MaxChatAttachmentBytes {
		httpx.Error(w, http.StatusBadRequest, "附件不得超过20MiB")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	attachment, err := teacher.ParseChatAttachment(ctx, r.PathValue("id"), header.Filename, r.FormValue("language"), data)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, attachment)
}

func (s *Server) chatMediaLock(slug string) *sync.Mutex {
	value, _ := s.chatMediaMu.LoadOrStore(slug, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (s *Server) handleChatAttachmentFile(w http.ResponseWriter, r *http.Request) {
	st, err := teacher.NewConversation(r.PathValue("id"))
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	a, data, err := st.AttachmentFile(r.PathValue("attachmentId"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "聊天附件不存在")
		return
	}
	w.Header().Set("Content-Type", a.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "inline")
	_, _ = w.Write(data)
}
