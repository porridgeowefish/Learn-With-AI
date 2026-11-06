package notestore

import (
	"context"
	"errors"
	conversation "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
	config "github.com/xmz14/lll/backend-go/internal/platform/config"
	"time"
)

type NoteTurn = conversation.NoteTurn
type AutoNotesSettings = config.AutoNotesSettings
type NoteUsage struct {
	InputTokens      int  `json:"inputTokens"`
	OutputTokens     int  `json:"outputTokens"`
	UsageReported    bool `json:"usageReported,omitempty"`
	CacheReadTokens  *int `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int `json:"cacheWriteTokens,omitempty"`
	CacheMissTokens  *int `json:"cacheMissTokens,omitempty"`
}
type NoteEntry struct {
	ID          string    `json:"id"`
	JobID       string    `json:"jobId"`
	CreatedAt   time.Time `json:"createdAt"`
	FromTurn    int       `json:"fromTurn"`
	ThroughTurn int       `json:"throughTurn"`
	Content     string    `json:"content"`
	ProviderID  string    `json:"providerId"`
	Model       string    `json:"model"`
}
type NoteJob struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	FromTurn    int        `json:"fromTurn"`
	ThroughTurn int        `json:"throughTurn"`
	Turns       []NoteTurn `json:"turns"`
	ProviderID  string     `json:"providerId"`
	Model       string     `json:"model"`
	NoteID      string     `json:"noteId,omitempty"`
	ErrorCode   string     `json:"errorCode,omitempty"`
	Retryable   bool       `json:"retryable"`
}
type NotesResource struct {
	Entries []NoteEntry `json:"entries"`
	Jobs    []NoteJob   `json:"jobs"`
}
type NoteGenerator func(context.Context, string, string, []NoteTurn) (string, NoteUsage, error)

var ErrNoteJobNotFound = errors.New("note job not found")
var ErrNoteJobRunning = errors.New("note job is already running or completed")
