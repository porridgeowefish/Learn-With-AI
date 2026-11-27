package httpserver

import (
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/sources"
)

const (
	maxStudyAttachmentBytes  = 100 << 20
	maxStudyAttachmentTokens = 3600
	maxStudyAttachments      = 32
)

type studyAttachment struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

func (s *Server) handleStudyAttachment(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxStudyAttachmentBytes+(64<<10))
	form, err := r.MultipartReader()
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "请选择要发送给学习助手的文件。")
		return
	}
	part, err := form.NextPart()
	if err != nil || part.FormName() != "file" || part.FileName() == "" {
		httpx.Error(w, http.StatusBadRequest, "请选择要发送给学习助手的文件。")
		return
	}
	defer part.Close()
	file, err := os.CreateTemp("", "lwa-study-attachment-*")
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "暂时无法接收附件。")
		return
	}
	defer os.Remove(file.Name())
	size, copyErr := io.Copy(file, io.LimitReader(part, maxStudyAttachmentBytes+1))
	closeErr := file.Close()
	if size > maxStudyAttachmentBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "单份附件不能超过 100 MiB。")
		return
	}
	if copyErr != nil || closeErr != nil {
		httpx.Error(w, http.StatusBadRequest, "这份附件没有上传完整，请重试。")
		return
	}
	name := filepath.Base(part.FileName())
	content, err := sources.ExtractDocumentText(r.Context(), file.Name(), name)
	if err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "未能读取附件正文。请尝试可复制文字的 PDF、文档或文本文件。")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, studyAttachment{Name: name, Text: ai.TruncateTokens(content, maxStudyAttachmentTokens)})
}
