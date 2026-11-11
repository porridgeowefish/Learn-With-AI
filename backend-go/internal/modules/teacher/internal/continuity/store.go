// Package continuity owns confirmed teaching boundaries and their derived knowledge.
package continuity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	conversation "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

type SourceMessage struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Text string `json:"text"`
}
type Summary struct {
	ID                string                `json:"id"`
	SessionID         string                `json:"teachingSessionId"`
	ConfirmationID    string                `json:"confirmationMessageId"`
	AcknowledgementID string                `json:"acknowledgementMessageId"`
	FromSeq           uint64                `json:"fromSeq"`
	ThroughSeq        uint64                `json:"throughSeq"`
	SourceHash        string                `json:"sourceHash"`
	Input             []SourceMessage       `json:"input,omitempty"`
	SourceProviderID  string                `json:"sourceProviderId,omitempty"`
	ProviderID        string                `json:"providerId,omitempty"`
	Model             string                `json:"model,omitempty"`
	Prompt            string                `json:"prompt,omitempty"`
	PromptReference   *promptpack.Reference `json:"promptReference,omitempty"`
	ConfirmationHash  string                `json:"confirmationHash"`
	PromptHash        string                `json:"promptHash,omitempty"`
	Status            string                `json:"status"`
	Concepts          []string              `json:"concepts,omitempty"`
	Scenario          string                `json:"scenario,omitempty"`
	Error             string                `json:"error,omitempty"`
	CreatedAt         time.Time             `json:"createdAt"`
	CompletedAt       *time.Time            `json:"completedAt,omitempty"`
}
type session struct {
	ID      string `json:"id"`
	FromSeq uint64 `json:"fromSeq"`
}
type state struct {
	SchemaVersion int       `json:"schemaVersion"`
	Active        *session  `json:"active,omitempty"`
	Summaries     []Summary `json:"summaries"`
}

var locks sync.Map

func lockFor(slug string) *sync.Mutex {
	root, _ := projects.ProjectRootForSlug(slug)
	v, _ := locks.LoadOrStore(root, &sync.Mutex{})
	return v.(*sync.Mutex)
}
func statePath(slug string) (string, error) {
	st, err := projects.ReadProjectState(slug)
	if err != nil {
		return "", err
	}
	if st.ProjectType != projects.ProjectTypeSystemLearning {
		return "", errors.New("summaries require a learning project")
	}
	root, err := projects.ProjectRootForSlug(slug)
	return filepath.Join(root, "teaching-summaries", "state.json"), err
}
func load(slug string) (state, error) {
	p, e := statePath(slug)
	if e != nil {
		return state{}, e
	}
	b, e := os.ReadFile(p)
	if errors.Is(e, os.ErrNotExist) {
		return state{SchemaVersion: 1, Summaries: []Summary{}}, nil
	}
	if e != nil {
		return state{}, e
	}
	var s state
	if json.Unmarshal(b, &s) != nil || s.SchemaVersion != 1 {
		return s, errors.New("invalid teaching summary state")
	}
	return s, nil
}
func save(slug string, s state) error {
	p, e := statePath(slug)
	if e != nil {
		return e
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	return filesystem.AtomicWriteFile(p, b, 0600)
}
func hash(in any) string {
	b, _ := json.Marshal(in)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func snapshot(slug string, from, to uint64) ([]SourceMessage, error) {
	c, e := conversation.New(slug)
	if e != nil {
		return nil, e
	}
	msgs, e := c.SequencedMessages()
	if e != nil {
		return nil, e
	}
	out := []SourceMessage{}
	for _, item := range msgs {
		m := item.Message
		if item.Seq < from || item.Seq > to || m.Status != "completed" || (m.Role != "teacher" && m.Role != "learner") {
			continue
		}
		var parts []string
		for _, b := range m.Blocks {
			if b.Type == "markdown" && strings.TrimSpace(b.Source) != "" {
				parts = append(parts, b.Source)
			}
		}
		if len(parts) > 0 {
			out = append(out, SourceMessage{m.ID, m.Role, strings.Join(parts, "\n")})
		}
	}
	return out, nil
}

// Begin is idempotent for ordinary turns and never generates a summary.
func Begin(slug string, seq uint64) error {
	mu := lockFor(slug)
	mu.Lock()
	defer mu.Unlock()
	s, e := load(slug)
	if e != nil {
		return e
	}
	if s.Active != nil {
		return nil
	}
	start := seq
	if len(s.Summaries) == 0 {
		start = 0
	}
	s.Active = &session{ID: idgen.New("lesson"), FromSeq: start}
	return save(slug, s)
}

// Confirm atomically seals one session and its pending job. Replayed confirmations
// return the same record, even when the learner sends another confirmation message.
func Confirm(slug, learnerID, ackID string, learnerSeq uint64, sourceProviderID ...string) (Summary, error) {
	mu := lockFor(slug)
	mu.Lock()
	defer mu.Unlock()
	s, e := load(slug)
	if e != nil {
		return Summary{}, e
	}
	for _, v := range s.Summaries {
		if v.ConfirmationID == learnerID {
			return v, nil
		}
	}
	if s.Active == nil && len(s.Summaries) > 0 {
		return s.Summaries[len(s.Summaries)-1], nil
	}
	if learnerSeq == 0 {
		return Summary{}, errors.New("confirmation must be a durable learner message")
	}
	c, e := conversation.New(slug)
	if e != nil {
		return Summary{}, e
	}
	m, ok, e := c.FindMessage(learnerID)
	if e != nil || !ok || m.Role != "learner" || m.Status != "completed" {
		return Summary{}, errors.New("invalid summary confirmation")
	}
	messages, e := c.SequencedMessages()
	if e != nil {
		return Summary{}, e
	}
	matched := false
	for _, item := range messages {
		if item.Message.ID == learnerID && item.Seq == learnerSeq {
			matched = true
		}
	}
	if !matched {
		return Summary{}, errors.New("confirmation sequence mismatch")
	}
	if s.Active == nil {
		s.Active = &session{ID: idgen.New("lesson"), FromSeq: 0}
	}
	input, e := snapshot(slug, s.Active.FromSeq, learnerSeq-1)
	if e != nil {
		return Summary{}, e
	}
	hasTeaching := false
	for _, m := range input {
		if m.Role == "teacher" {
			hasTeaching = true
		}
	}
	if !hasTeaching {
		return Summary{}, errors.New("no completed teaching to summarize")
	}
	v := Summary{ID: idgen.New("summary"), SessionID: s.Active.ID, ConfirmationID: learnerID, AcknowledgementID: ackID, FromSeq: s.Active.FromSeq, ThroughSeq: learnerSeq - 1, SourceHash: hash(input), Input: input, Status: "pending", CreatedAt: time.Now().UTC()}
	if len(sourceProviderID) > 0 {
		v.SourceProviderID = sourceProviderID[0]
	}
	v.ConfirmationHash = hash(m)
	s.Summaries = append(s.Summaries, v)
	s.Active = nil
	if e = save(slug, s); e != nil {
		return Summary{}, e
	}
	return v, nil
}
func valid(slug string, v Summary) bool {
	input, e := snapshot(slug, v.FromSeq, v.ThroughSeq)
	if e != nil || hash(input) != v.SourceHash {
		return false
	}
	c, e := conversation.New(slug)
	if e != nil {
		return false
	}
	m, ok, e := c.FindMessage(v.ConfirmationID)
	return e == nil && ok && m.Role == "learner" && m.Status == "completed" && hash(m) == v.ConfirmationHash
}
func List(slug string) ([]Summary, error) {
	mu := lockFor(slug)
	mu.Lock()
	defer mu.Unlock()
	s, e := load(slug)
	if e != nil {
		return nil, e
	}
	c, e := conversation.New(slug)
	if e != nil {
		return nil, e
	}
	messages, e := c.SequencedMessages()
	if e != nil {
		return nil, e
	}
	byID := map[string]conversation.Message{}
	for _, item := range messages {
		byID[item.Message.ID] = item.Message
	}
	out := make([]Summary, 0, len(s.Summaries))
	for _, v := range s.Summaries {
		input := []SourceMessage{}
		for _, item := range messages {
			m := item.Message
			if item.Seq < v.FromSeq || item.Seq > v.ThroughSeq || m.Status != "completed" || (m.Role != "teacher" && m.Role != "learner") {
				continue
			}
			parts := []string{}
			for _, block := range m.Blocks {
				if block.Type == "markdown" && strings.TrimSpace(block.Source) != "" {
					parts = append(parts, block.Source)
				}
			}
			if len(parts) > 0 {
				input = append(input, SourceMessage{m.ID, m.Role, strings.Join(parts, "\n")})
			}
		}
		m, ok := byID[v.ConfirmationID]
		valid := ok && m.Role == "learner" && m.Status == "completed" && hash(m) == v.ConfirmationHash && hash(input) == v.SourceHash
		if v.Status != "revoked" && !valid {
			v.Status = "superseded"
		}
		v.Input = nil
		v.Prompt = ""
		v.PromptReference = nil
		out = append(out, v)
	}
	return out, nil
}
func update(slug, id string, fn func(*Summary) error) error {
	mu := lockFor(slug)
	mu.Lock()
	defer mu.Unlock()
	s, e := load(slug)
	if e != nil {
		return e
	}
	for i := range s.Summaries {
		if s.Summaries[i].ID == id {
			if e = fn(&s.Summaries[i]); e != nil {
				return e
			}
			return save(slug, s)
		}
	}
	return errors.New("summary not found")
}
func Retry(slug, id string) error {
	return update(slug, id, func(v *Summary) error {
		if v.Status != "failed" && v.Status != "interrupted" {
			return errors.New("summary cannot be retried")
		}
		if !valid(slug, *v) {
			return errors.New("summary source changed")
		}
		v.Status = "pending"
		v.Error = ""
		return nil
	})
}
func Revoke(slug, id string) error {
	return update(slug, id, func(v *Summary) error { v.Status = "revoked"; v.Concepts = nil; v.Scenario = ""; return nil })
}

// Knowledge only returns confirmed, still valid concepts, never source history.
func Knowledge(slug string) (string, error) {
	items, e := List(slug)
	if e != nil {
		return "", e
	}
	var out []string
	for _, v := range items {
		if v.Status != "completed" {
			continue
		}
		line := "概念：" + strings.Join(v.Concepts, "、") + "。"
		if v.Scenario != "" {
			line += " 场景：" + v.Scenario
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), nil
}
