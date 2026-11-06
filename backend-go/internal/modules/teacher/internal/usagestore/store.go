// Package usagestore persists provider-reported teacher token usage. It is
// intentionally separate from conversation events: usage is operational
// telemetry, while conversation history remains learner-facing evidence.
package usagestore

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
)

type Record struct {
	ConversationID   string    `json:"conversationId"`
	UnitID           string    `json:"unitId"`
	ResponseID       string    `json:"responseId"`
	ProviderID       string    `json:"providerId,omitempty"`
	InputTokens      int       `json:"inputTokens"`
	OutputTokens     int       `json:"outputTokens"`
	UsageReported    bool      `json:"usageReported,omitempty"`
	CacheReadTokens  *int      `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int      `json:"cacheWriteTokens,omitempty"`
	CacheMissTokens  *int      `json:"cacheMissTokens,omitempty"`
	OccurredAt       time.Time `json:"occurredAt"`
}

type Conversation struct {
	ProjectSlug      string    `json:"projectSlug"`
	Title            string    `json:"title"`
	ConversationID   string    `json:"conversationId"`
	UnitID           string    `json:"unitId"`
	InputTokens      int       `json:"inputTokens"`
	OutputTokens     int       `json:"outputTokens"`
	CacheReadTokens  *int      `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int      `json:"cacheWriteTokens,omitempty"`
	Turns            []Record  `json:"turns"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

var lock sync.Mutex

func Append(slug string, record Record) error { return appendLedger(slug, record, "usage.jsonl") }
func AppendNote(slug string, record Record) error {
	return appendLedger(slug, record, "note-usage.jsonl")
}
func appendLedger(slug string, record Record, filename string) error {
	if record.ResponseID == "" || record.ConversationID == "" || record.UnitID == "" || (!record.UsageReported && record.InputTokens == 0 && record.OutputTokens == 0 && record.CacheReadTokens == nil && record.CacheWriteTokens == nil) {
		return nil
	}
	if record.OccurredAt.IsZero() {
		record.OccurredAt = time.Now().UTC()
	}
	root, err := workspace.ProjectRootForSlug(slug)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "conversation"), 0o755); err != nil {
		return err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	lock.Lock()
	defer lock.Unlock()
	file, err := os.OpenFile(filepath.Join(root, "conversation", filename), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(encoded, '\n'))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func ListAll() ([]Conversation, error) {
	projects, err := workspace.IndexAll()
	if err != nil {
		return nil, err
	}
	rows := make([]Conversation, 0)
	for _, project := range projects {
		if project.ProjectType != workspace.ProjectTypeSystemLearning {
			continue
		}
		root, rootErr := workspace.ProjectRootForSlug(project.Slug)
		if rootErr != nil {
			continue
		}
		records, readErr := read(filepath.Join(root, "conversation", "usage.jsonl"))
		if readErr != nil || len(records) == 0 {
			continue
		}
		byConversation := map[string]*Conversation{}
		for _, record := range records {
			row := byConversation[record.ConversationID]
			if row == nil {
				row = &Conversation{ProjectSlug: project.Slug, Title: project.Title, ConversationID: record.ConversationID, UnitID: record.UnitID, Turns: []Record{}}
				byConversation[record.ConversationID] = row
			}
			row.InputTokens += record.InputTokens
			row.OutputTokens += record.OutputTokens
			addOptionalTokens(&row.CacheReadTokens, record.CacheReadTokens)
			addOptionalTokens(&row.CacheWriteTokens, record.CacheWriteTokens)
			row.Turns = append(row.Turns, record)
			if record.OccurredAt.After(row.UpdatedAt) {
				row.UpdatedAt = record.OccurredAt
			}
		}
		for _, row := range byConversation {
			rows = append(rows, *row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UpdatedAt.After(rows[j].UpdatedAt) })
	return rows, nil
}

// OptionalTokens never substitutes zero for an unreported provider metric.
func OptionalTokens(metrics map[string]int, name string) *int {
	value, known := metrics[name]
	if !known || value < 0 {
		return nil
	}
	return &value
}

func addOptionalTokens(total **int, value *int) {
	if value == nil || *value < 0 {
		return
	}
	if *total == nil {
		*total = new(int)
	}
	**total += *value
}

func read(path string) ([]Record, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	items := []Record{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item Record
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, err
		}
		if item.ResponseID != "" {
			items = append(items, item)
		}
	}
	return items, scanner.Err()
}
