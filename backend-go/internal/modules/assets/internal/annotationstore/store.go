// Package annotationstore owns append-only annotations and Ask-AI threads for
// the canonical review asset. The JSONL event log is authoritative; projections
// are rebuilt on every store open and can be discarded safely.
package annotationstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	"github.com/xmz14/lll/backend-go/internal/modules/assets/internal/assetstore"
	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
)

const SchemaVersion = 1

type State string

const (
	StateOpen     State = "open"
	StateAsked    State = "asked"
	StateResolved State = "resolved"
	StateDeleted  State = "deleted"
)

type Anchors struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"` // learner | assistant
	Content   string    `json:"content"`
	Status    string    `json:"status,omitempty"` // completed | interrupted | failed
	CreatedAt time.Time `json:"createdAt"`
}

type Ask struct {
	Messages     []Message `json:"messages,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	SummaryState string    `json:"summaryState,omitempty"`
	ProviderID   string    `json:"providerId,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

type Annotation struct {
	SchemaVersion    int       `json:"schemaVersion"`
	AnnotationID     string    `json:"annotationId"`
	AssetID          string    `json:"assetId"`
	AssetVersionID   string    `json:"assetVersionId"`
	QuoteSnapshot    string    `json:"quoteSnapshot"`
	Anchors          Anchors   `json:"anchors"`
	Note             string    `json:"note,omitempty"`
	Status           State     `json:"status"`
	Detached         bool      `json:"detached,omitempty"`
	LegacySource     bool      `json:"legacySource,omitempty"`
	LegacyExternalID string    `json:"legacyExternalId,omitempty"`
	SourceArtifactID string    `json:"sourceArtifactId,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	Ask              *Ask      `json:"ask,omitempty"`
}

type CreateInput struct {
	AnnotationID     string
	AssetVersionID   string
	QuoteSnapshot    string
	Anchors          Anchors
	Note             string
	LegacyExternalID string
	SourceArtifactID string
	Status           State
	CreatedAt        time.Time
	Ask              *Ask
}

type Event struct {
	SchemaVersion int         `json:"schemaVersion"`
	Seq           uint64      `json:"seq"`
	EventID       string      `json:"eventId"`
	Type          string      `json:"type"`
	AnnotationID  string      `json:"annotationId"`
	At            time.Time   `json:"at"`
	Annotation    *Annotation `json:"annotation,omitempty"`
}

type Store struct {
	slug string
	root string
	path string
	mu   *sync.Mutex
}

var locks sync.Map

func New(slug string) (*Store, error) {
	if !workspace.ValidateSlug(slug) {
		return nil, errors.New("invalid project slug")
	}
	root, err := workspace.ProjectRootForSlug(slug)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, "assets", "review", "annotations.jsonl")
	v, _ := locks.LoadOrStore(path, &sync.Mutex{})
	s := &Store{slug: slug, root: root, path: path, mu: v.(*sync.Mutex)}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if f, err := os.OpenFile(path, os.O_CREATE, 0o644); err != nil {
		return nil, err
	} else {
		_ = f.Close()
	}
	return s, nil
}

func (s *Store) List(includeDeleted bool) ([]Annotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projection, _, err := s.projectLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Annotation, 0, len(projection))
	for _, annotation := range projection {
		if includeDeleted || annotation.Status != StateDeleted {
			out = append(out, annotation)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) Get(id string) (Annotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projection, _, err := s.projectLocked()
	if err != nil {
		return Annotation{}, err
	}
	annotation, ok := projection[id]
	if !ok || annotation.Status == StateDeleted {
		return Annotation{}, os.ErrNotExist
	}
	return annotation, nil
}

func (s *Store) Create(in CreateInput) (Annotation, error) {
	if strings.TrimSpace(in.QuoteSnapshot) == "" {
		return Annotation{}, errors.New("quote snapshot is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	projection, seq, err := s.projectLocked()
	if err != nil {
		return Annotation{}, err
	}
	id := strings.TrimSpace(in.AnnotationID)
	if id == "" {
		id = idgen.New("ann")
	}
	if _, exists := projection[id]; exists {
		return Annotation{}, fmt.Errorf("annotation already exists: %s", id)
	}
	assets, err := assetstore.New(s.slug)
	if err != nil {
		return Annotation{}, err
	}
	body, err := assets.Get("review")
	if err != nil {
		return Annotation{}, err
	}
	versionID := strings.TrimSpace(in.AssetVersionID)
	if versionID == "" {
		versionID = body.Meta.CurrentVersionID
	}
	now := in.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state := in.Status
	if state == "" {
		state = StateOpen
	}
	annotation := Annotation{SchemaVersion: SchemaVersion, AnnotationID: id, AssetID: body.Meta.AssetID, AssetVersionID: versionID, QuoteSnapshot: strings.TrimSpace(in.QuoteSnapshot), Anchors: in.Anchors, Note: in.Note, Status: state, LegacyExternalID: in.LegacyExternalID, SourceArtifactID: in.SourceArtifactID, CreatedAt: now, UpdatedAt: now, Ask: normalizeAsk(in.Ask)}
	if err := s.appendLocked(Event{SchemaVersion: SchemaVersion, Seq: seq + 1, EventID: idgen.New("aevt"), Type: "annotation-created", AnnotationID: id, At: now, Annotation: &annotation}); err != nil {
		return Annotation{}, err
	}
	return annotation, nil
}

func (s *Store) Update(id string, note *string, state *State) (Annotation, error) {
	return s.mutate(id, "annotation-updated", func(annotation *Annotation) error {
		if note != nil {
			annotation.Note = *note
		}
		if state != nil {
			switch *state {
			case StateOpen, StateAsked, StateResolved, StateDeleted:
				annotation.Status = *state
			default:
				return errors.New("invalid annotation status")
			}
		}
		return nil
	})
}

func (s *Store) Delete(id string) error {
	state := StateDeleted
	_, err := s.Update(id, nil, &state)
	return err
}

func (s *Store) AppendMessage(id string, message Message) (Annotation, error) {
	return s.mutate(id, "annotation-message-recorded", func(annotation *Annotation) error {
		if message.Role == "user" {
			message.Role = "learner"
		}
		if message.Role != "learner" && message.Role != "assistant" {
			return errors.New("invalid annotation message role")
		}
		if strings.TrimSpace(message.Content) == "" {
			return errors.New("annotation message content is required")
		}
		if message.ID == "" {
			message.ID = idgen.New("amsg")
		}
		if message.CreatedAt.IsZero() {
			message.CreatedAt = time.Now().UTC()
		}
		if message.Status == "" {
			message.Status = "completed"
		}
		if annotation.Ask == nil {
			annotation.Ask = &Ask{}
		}
		annotation.Ask.Messages = append(annotation.Ask.Messages, message)
		annotation.Ask.SummaryState = "idle"
		annotation.Ask.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (s *Store) SetAskSummary(id, summary, state string) (Annotation, error) {
	return s.mutate(id, "annotation-summary-updated", func(annotation *Annotation) error {
		if annotation.Ask == nil {
			annotation.Ask = &Ask{}
		}
		annotation.Ask.Summary = summary
		annotation.Ask.SummaryState = state
		annotation.Ask.UpdatedAt = time.Now().UTC()
		if state == "done" {
			annotation.Status = StateAsked
		}
		return nil
	})
}

func (s *Store) mutate(id, eventType string, fn func(*Annotation) error) (Annotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projection, seq, err := s.projectLocked()
	if err != nil {
		return Annotation{}, err
	}
	annotation, ok := projection[id]
	if !ok || annotation.Status == StateDeleted {
		return Annotation{}, os.ErrNotExist
	}
	if err := fn(&annotation); err != nil {
		return Annotation{}, err
	}
	annotation.UpdatedAt = time.Now().UTC()
	if err := s.appendLocked(Event{SchemaVersion: SchemaVersion, Seq: seq + 1, EventID: idgen.New("aevt"), Type: eventType, AnnotationID: id, At: annotation.UpdatedAt, Annotation: &annotation}); err != nil {
		return Annotation{}, err
	}
	return annotation, nil
}

func (s *Store) projectLocked() (map[string]Annotation, uint64, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	projection := map[string]Annotation{}
	var seq uint64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		data := scanner.Bytes()
		if len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(data, &event); err != nil {
			// A power loss may leave only the final JSONL record truncated.
			if !scanner.Scan() {
				break
			}
			return nil, 0, fmt.Errorf("decode annotation event line %d: %w", line, err)
		}
		if event.Seq <= seq || event.Annotation == nil || event.AnnotationID == "" {
			return nil, 0, fmt.Errorf("invalid annotation event at line %d", line)
		}
		seq = event.Seq
		projection[event.AnnotationID] = *event.Annotation
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return projection, seq, nil
}

func (s *Store) appendLocked(event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func normalizeAsk(ask *Ask) *Ask {
	if ask == nil {
		return nil
	}
	copyAsk := *ask
	copyAsk.Messages = append([]Message(nil), ask.Messages...)
	for i := range copyAsk.Messages {
		if copyAsk.Messages[i].Role == "user" {
			copyAsk.Messages[i].Role = "learner"
		}
		if copyAsk.Messages[i].ID == "" {
			copyAsk.Messages[i].ID = idgen.New("amsg")
		}
		if copyAsk.Messages[i].CreatedAt.IsZero() {
			copyAsk.Messages[i].CreatedAt = time.Now().UTC()
		}
		if copyAsk.Messages[i].Status == "" {
			copyAsk.Messages[i].Status = "completed"
		}
	}
	return &copyAsk
}
