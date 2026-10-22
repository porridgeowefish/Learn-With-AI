package conversationstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/speech"
)

const MaxAttachmentBytes = 20 << 20

type Attachment struct {
	AttachmentID string    `json:"attachmentId"`
	Kind         string    `json:"kind"`
	Name         string    `json:"name"`
	MediaType    string    `json:"mediaType"`
	Status       string    `json:"status"`
	Text         string    `json:"text"`
	CreatedAt    time.Time `json:"createdAt"`
}
type MediaExtractor func(context.Context, string, string, []byte) (string, error)

var attachmentIDPattern = regexp.MustCompile(`^chat_[a-zA-Z0-9_-]{8,80}$`)

func (s *Store) AddAttachment(ctx context.Context, name, language string, data []byte, extract MediaExtractor) (Attachment, error) {
	if !speech.ValidLanguage(language) {
		return Attachment{}, errors.New("目前只支持中文和英文")
	}
	if len(data) == 0 || len(data) > MaxAttachmentBytes {
		return Attachment{}, errors.New("附件大小必须在20MiB以内")
	}
	name = strings.Map(func(r rune) rune {
		if r < ' ' || r == 127 {
			return -1
		}
		return r
	}, filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if len(name) > 240 || name == "" || !utf8.ValidString(name) {
		return Attachment{}, errors.New("附件名称无效或过长")
	}
	a := Attachment{AttachmentID: idgen.New("chat"), Name: name, CreatedAt: time.Now().UTC()}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		if err := speech.ValidateWave(data); err != nil {
			return Attachment{}, err
		}
		a.Kind, a.MediaType = "audio", "audio/wav"
	} else {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return Attachment{}, errors.New("请使用PNG、JPEG、GIF图片或PCM WAV音频")
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
			return Attachment{}, errors.New("图片超过1600万像素，请缩小后上传")
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return Attachment{}, errors.New("图片无法解码")
		}
		var out bytes.Buffer
		if err = png.Encode(&out, img); err != nil {
			return Attachment{}, errors.New("图片转换失败")
		}
		data = out.Bytes()
		if len(data) > 8<<20 {
			return Attachment{}, errors.New("图片解码后超过8MiB，请缩小后上传")
		}
		a.Kind, a.MediaType = "image", "image/png"
	}
	text, err := extract(ctx, a.Kind, language, data)
	if err != nil {
		return Attachment{}, err
	}
	if err := ctx.Err(); err != nil {
		return Attachment{}, errors.New("附件解析已取消")
	}
	text = strings.TrimSpace(text)
	if len(text) > 32<<10 || !utf8.ValidString(text) {
		return Attachment{}, errors.New("提取文字超过限制，请分批发送")
	}
	a.Text, a.Status = text, "ready"
	if text == "" {
		a.Status = "empty"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Recognition finishes before persistence. Never recreate a project deleted while it ran.
	if _, err := workspace.ReadProjectState(s.slug); err != nil {
		return Attachment{}, errors.New("项目已不存在")
	}
	if err := s.pruneAttachmentsLocked(int64(len(data))); err != nil {
		return Attachment{}, err
	}
	dir := filepath.Join(s.root, "conversation", "attachments", a.AttachmentID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Attachment{}, err
	}
	if err := filesystem.AtomicWriteFile(filepath.Join(dir, "original"), data, 0600); err != nil {
		_ = os.RemoveAll(dir)
		return Attachment{}, err
	}
	encoded, _ := json.Marshal(a)
	if err := filesystem.AtomicWriteFile(filepath.Join(dir, "attachment.json"), encoded, 0600); err != nil {
		_ = os.RemoveAll(dir)
		return Attachment{}, err
	}
	return a, nil
}

func (s *Store) attachmentLocked(id string) (Attachment, error) {
	if !attachmentIDPattern.MatchString(id) {
		return Attachment{}, errors.New("无效的聊天附件")
	}
	data, err := os.ReadFile(filepath.Join(s.root, "conversation", "attachments", id, "attachment.json"))
	if err != nil {
		return Attachment{}, errors.New("聊天附件不存在或不属于当前项目")
	}
	var a Attachment
	if len(data) > 40<<10 || json.Unmarshal(data, &a) != nil || a.AttachmentID != id || (a.Kind != "image" && a.Kind != "audio") || (a.Status != "ready" && a.Status != "empty") {
		return Attachment{}, errors.New("聊天附件记录无效")
	}
	return a, nil
}

func (s *Store) AttachmentFile(id string) (Attachment, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.attachmentLocked(id)
	if err != nil {
		return a, nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.root, "conversation", "attachments", id, "original"))
	return a, data, err
}

func (s *Store) AttachmentBlocks(ids []string) ([]Block, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attachmentBlocksLocked(ids)
}
func (s *Store) attachmentBlocksLocked(ids []string) ([]Block, error) {
	if len(ids) > 4 {
		return nil, errors.New("每条消息最多4个聊天附件")
	}
	seen := map[string]bool{}
	var blocks []Block
	for _, id := range ids {
		if seen[id] {
			return nil, errors.New("聊天附件不能重复")
		}
		seen[id] = true
		a, err := s.attachmentLocked(id)
		if err != nil {
			return nil, err
		}
		text := a.Text
		if a.Status == "empty" {
			if a.Kind == "image" {
				text = "[图片未提取到文字。请告知学习者：当前仅支持文字提取，未识别到文字；无法判断图片内容。]"
			} else {
				text = "[音频未识别到语音文字。请告知学习者并邀请重新录音。]"
			}
		}
		blocks = append(blocks, Block{Type: "chat-" + a.Kind, ArtifactRef: id, Name: a.Name, Source: text})
	}
	return blocks, nil
}

// Unsent originals expire after a day. Sent/queued attachments remain owned by history.
func (s *Store) pruneAttachmentsLocked(incoming int64) error {
	_, events, err := s.loadLocked()
	if err != nil {
		return err
	}
	referenced := map[string]bool{}
	for _, e := range events {
		if e.Type == "message-recorded" {
			var m Message
			if json.Unmarshal(e.Data, &m) == nil {
				for _, b := range m.Blocks {
					if strings.HasPrefix(b.Type, "chat-") {
						referenced[b.ArtifactRef] = true
					}
				}
			}
		}
	}
	for _, q := range replayQueue(events) {
		for _, b := range q.MediaBlocks {
			referenced[b.ArtifactRef] = true
		}
	}
	root := filepath.Join(s.root, "conversation", "attachments")
	entries, _ := os.ReadDir(root)
	var total int64
	for _, entry := range entries {
		if !entry.IsDir() || !attachmentIDPattern.MatchString(entry.Name()) {
			continue
		}
		a, err := s.attachmentLocked(entry.Name())
		if err != nil {
			continue
		}
		if !referenced[a.AttachmentID] && time.Since(a.CreatedAt) > 24*time.Hour {
			_ = os.RemoveAll(filepath.Join(root, a.AttachmentID))
			continue
		}
		if info, err := os.Stat(filepath.Join(root, a.AttachmentID, "original")); err == nil {
			total += info.Size()
		}
	}
	if total+incoming > 256<<20 {
		return errors.New("本对话媒体附件已达到256MiB上限")
	}
	return nil
}
