package conversationstore

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
	sources "github.com/xmz14/lll/backend-go/internal/modules/sources"
)

const SchemaVersion = 1

type Block struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Source      string `json:"source,omitempty"`
	ArtifactRef string `json:"artifactRef,omitempty"`
	Name        string `json:"name,omitempty"`
}

type Message struct {
	ID              string                   `json:"id"`
	Role            string                   `json:"role"`
	Status          string                   `json:"status"`
	Blocks          []Block                  `json:"blocks"`
	OperationID     string                   `json:"operationId,omitempty"`
	CreatedAt       time.Time                `json:"createdAt"`
	CompletedAt     time.Time                `json:"completedAt,omitempty"`
	SourceCitations []sources.SourceCitation `json:"sourceCitations,omitempty"`
	ReadingCoverage []ReadingCoverage        `json:"readingCoverage,omitempty"`
}

type Event struct {
	SchemaVersion int             `json:"schemaVersion"`
	Seq           uint64          `json:"seq"`
	EventID       string          `json:"eventId"`
	Type          string          `json:"type"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Data          json.RawMessage `json:"data"`
}

type Meta struct {
	SchemaVersion       int       `json:"schemaVersion"`
	ID                  string    `json:"id"`
	UnitID              string    `json:"unitId"`
	LatestSeq           uint64    `json:"latestSeq"`
	LatestEventID       string    `json:"latestEventId,omitempty"`
	EventCount          uint64    `json:"eventCount"`
	CompactedThroughSeq uint64    `json:"compactedThroughSeq"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type Unit struct {
	SchemaVersion   int       `json:"schemaVersion"`
	UnitID          string    `json:"unitId"`
	ConversationID  string    `json:"conversationId"`
	ProjectSlug     string    `json:"projectSlug"`
	ActiveAssetKeys []string  `json:"activeAssetKeys"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Projection struct {
	ConversationID   string      `json:"conversationId"`
	UnitID           string      `json:"unitId"`
	LatestSeq        uint64      `json:"latestSeq"`
	PageFromSeq      uint64      `json:"pageFromSeq,omitempty"`
	PageThroughSeq   uint64      `json:"pageThroughSeq"`
	HasMore          bool        `json:"hasMore"`
	HasPrevious      bool        `json:"hasPrevious,omitempty"`
	TotalMessages    int         `json:"totalMessages"`
	Messages         []Message   `json:"messages"`
	TaskLinks        []TaskLink  `json:"taskLinks"`
	Queue            []QueueItem `json:"queue"`
	QueuePaused      bool        `json:"queuePaused"`
	LatestResponseID string      `json:"latestResponseId,omitempty"`
}

// QueueItem is one learner message waiting for the current turn to finish.
type QueueItem struct {
	QueueID        string    `json:"queueId"`
	Content        string    `json:"content"`
	AttachmentRefs []string  `json:"attachmentRefs,omitempty"`
	MediaBlocks    []Block   `json:"mediaBlocks,omitempty"`
	OperationID    string    `json:"operationId,omitempty"`
	ProviderID     string    `json:"providerId,omitempty"`
	QueuedAt       time.Time `json:"queuedAt"`
}

// PromotedTurn is the durable result of moving a queue item into a real turn.
type PromotedTurn struct {
	Item       QueueItem
	Learner    Message
	LearnerSeq uint64
	Mode       string // "auto" | "steer" | "manual"
}

type TaskLink struct {
	MessageID  string `json:"messageId"`
	TaskID     string `json:"taskId"`
	ToolCallID string `json:"toolCallId,omitempty"`
}

type SequencedMessage struct {
	Seq     uint64
	EventID string
	Message Message
}

type ResponseRecord struct {
	ResponseID       string
	TeacherMessageID string
	Message          *Message
	Active           bool
}

type Store struct {
	slug string
	root string
	mu   *sync.Mutex
}

var locks sync.Map

func New(slug string) (*Store, error) {
	if !workspace.ValidateSlug(slug) {
		return nil, errors.New("invalid project slug")
	}
	state, err := workspace.ReadProjectState(slug)
	if err != nil {
		return nil, err
	}
	if state.ProjectType != workspace.ProjectTypeSystemLearning {
		return nil, errors.New("teacher conversation requires system-learning project")
	}
	root, err := workspace.ProjectRootForSlug(slug)
	if err != nil {
		return nil, err
	}
	value, _ := locks.LoadOrStore(root, &sync.Mutex{})
	s := &Store{slug: slug, root: root, mu: value.(*sync.Mutex)}
	if err := s.ensure(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	convDir := filepath.Join(s.root, "conversation")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		return err
	}
	unitPath := filepath.Join(s.root, "unit.json")
	metaPath := filepath.Join(convDir, "conversation.json")
	if _, err := os.Stat(unitPath); err == nil {
		if _, err := os.Stat(metaPath); err == nil {
			return nil
		}
	}
	now := time.Now().UTC()
	unit := Unit{SchemaVersion: SchemaVersion, UnitID: idgen.New("unit"), ConversationID: idgen.New("conv"), ProjectSlug: s.slug, ActiveAssetKeys: []string{"review", "practice", "notes"}, CreatedAt: now, UpdatedAt: now}
	if data, err := os.ReadFile(unitPath); err == nil {
		_ = json.Unmarshal(data, &unit)
	}
	meta := Meta{SchemaVersion: SchemaVersion, ID: unit.ConversationID, UnitID: unit.UnitID, CreatedAt: now, UpdatedAt: now}
	if err := writeJSON(unitPath, unit); err != nil {
		return err
	}
	if err := writeJSON(metaPath, meta); err != nil {
		return err
	}
	events := filepath.Join(convDir, "events.jsonl")
	f, err := os.OpenFile(events, os.O_CREATE, 0o644)
	if err == nil {
		err = f.Close()
	}
	return err
}

func (s *Store) Unit() (Unit, error) {
	var u Unit
	err := readJSON(filepath.Join(s.root, "unit.json"), &u)
	return u, err
}

func (s *Store) Meta() (Meta, error) {
	var m Meta
	err := readJSON(filepath.Join(s.root, "conversation", "conversation.json"), &m)
	return m, err
}

func (s *Store) Append(eventType string, data any) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendLocked(eventType, data)
}

// ClaimOpening is durable before dispatch, including across a process restart.
// A failed dispatched opening is not silently retried or billed again.
func (s *Store) ClaimOpening() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return false, err
	}
	for _, event := range events {
		if event.Type == "teacher-opening-requested" || event.Type == "teacher-response-started" {
			return false, nil
		}
	}
	_, err = s.appendLocked("teacher-opening-requested", map[string]any{"operationId": "teacher-opening-v1"})
	return err == nil, err
}

func (s *Store) appendLocked(eventType string, data any) (Event, error) {
	meta, events, err := s.loadLocked()
	if err != nil {
		return Event{}, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	e := Event{SchemaVersion: SchemaVersion, Seq: meta.LatestSeq + 1, EventID: idgen.New("event"), Type: eventType, OccurredAt: time.Now().UTC(), Data: raw}
	line, err := json.Marshal(e)
	if err != nil {
		return Event{}, err
	}
	f, err := os.OpenFile(filepath.Join(s.root, "conversation", "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Event{}, err
	}
	if _, err = f.Write(append(line, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return Event{}, err
	}
	if closeErr != nil {
		return Event{}, closeErr
	}
	meta.LatestSeq = e.Seq
	meta.LatestEventID = e.EventID
	meta.EventCount = uint64(len(events) + 1)
	meta.UpdatedAt = e.OccurredAt
	if err := writeJSON(filepath.Join(s.root, "conversation", "conversation.json"), meta); err != nil {
		return Event{}, err
	}
	return e, nil
}

func (s *Store) AppendMessage(role, status, operationID string, blocks []Block) (Message, Event, error) {
	if role != "learner" && role != "teacher" && role != "system" {
		return Message{}, Event{}, errors.New("invalid message role")
	}
	for i := range blocks {
		if blocks[i].ID == "" {
			blocks[i].ID = idgen.New("blk")
		}
	}
	msg := Message{ID: idgen.New("msg"), Role: role, Status: status, Blocks: blocks, OperationID: operationID, CreatedAt: time.Now().UTC()}
	if status != "streaming" {
		msg.CompletedAt = msg.CreatedAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if operationID != "" {
		_, events, err := s.loadLocked()
		if err != nil {
			return Message{}, Event{}, err
		}
		for _, event := range events {
			if event.Type != "message-recorded" {
				continue
			}
			var existing Message
			if json.Unmarshal(event.Data, &existing) == nil && existing.OperationID == operationID {
				return existing, event, nil
			}
		}
	}
	e, err := s.appendLocked("message-recorded", msg)
	return msg, e, err
}

// RecordMessage persists a message whose identity was reserved before a
// streaming response started. It is also useful for importing durable history.
func (s *Store) RecordMessage(msg Message) (Event, error) {
	if msg.ID == "" || (msg.Role != "learner" && msg.Role != "teacher" && msg.Role != "system") {
		return Event{}, errors.New("invalid message")
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if msg.Status != "streaming" && msg.CompletedAt.IsZero() {
		msg.CompletedAt = time.Now().UTC()
	}
	for i := range msg.Blocks {
		if msg.Blocks[i].ID == "" {
			msg.Blocks[i].ID = idgen.New("blk")
		}
	}
	return s.Append("message-recorded", msg)
}

func (s *Store) Read(afterSeq uint64, limit int) (Projection, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, events, err := s.loadLocked()
	if err != nil {
		return Projection{}, err
	}
	hidden := hiddenTeacherMessages(events)
	proj := Projection{ConversationID: meta.ID, UnitID: meta.UnitID, LatestSeq: meta.LatestSeq, Messages: []Message{}, TaskLinks: []TaskLink{}, Queue: replayQueue(events), QueuePaused: replayQueuePaused(events), LatestResponseID: replayResponses(events).latest}
	for _, event := range events {
		if event.Type != "message-recorded" {
			continue
		}
		var msg Message
		if json.Unmarshal(event.Data, &msg) == nil && !(msg.Role == "teacher" && hidden[msg.ID]) {
			proj.TotalMessages++
		}
	}
	count := 0
	for _, event := range events {
		if event.Seq <= afterSeq {
			continue
		}
		if count >= limit {
			proj.HasMore = true
			break
		}
		proj.PageThroughSeq = event.Seq
		if proj.PageFromSeq == 0 {
			proj.PageFromSeq = event.Seq
		}
		switch event.Type {
		case "message-recorded":
			var msg Message
			if json.Unmarshal(event.Data, &msg) == nil && !(msg.Role == "teacher" && hidden[msg.ID]) {
				proj.Messages = append(proj.Messages, msg)
			}
		case "task-linked":
			var link TaskLink
			if json.Unmarshal(event.Data, &link) == nil {
				proj.TaskLinks = append(proj.TaskLinks, link)
			}
		}
		count++
	}
	return proj, nil
}

// ReadRecent returns the newest message page before beforeSeq. A zero cursor
// means "start at the tail". The page is message-based (not event-based), so
// task links remain attached even when their event was recorded before the
// corresponding teacher message. This endpoint is intended for interactive
// clients that must not download and render an entire long conversation just
// to open the teacher view.
func (s *Store) ReadRecent(beforeSeq uint64, limit int) (Projection, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, events, err := s.loadLocked()
	if err != nil {
		return Projection{}, err
	}
	type sequenced struct {
		seq     uint64
		message Message
	}
	hidden := hiddenTeacherMessages(events)
	messages := make([]sequenced, 0)
	links := make([]TaskLink, 0)
	for _, event := range events {
		switch event.Type {
		case "message-recorded":
			var message Message
			if json.Unmarshal(event.Data, &message) == nil && !(message.Role == "teacher" && hidden[message.ID]) {
				messages = append(messages, sequenced{seq: event.Seq, message: message})
			}
		case "task-linked":
			var link TaskLink
			if json.Unmarshal(event.Data, &link) == nil {
				links = append(links, link)
			}
		}
	}
	upper := beforeSeq
	if upper == 0 || upper > meta.LatestSeq+1 {
		upper = meta.LatestSeq + 1
	}
	end := len(messages)
	for end > 0 && messages[end-1].seq >= upper {
		end--
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	projection := Projection{
		ConversationID:   meta.ID,
		UnitID:           meta.UnitID,
		LatestSeq:        meta.LatestSeq,
		HasPrevious:      start > 0,
		TotalMessages:    len(messages),
		Messages:         []Message{},
		TaskLinks:        []TaskLink{},
		Queue:            replayQueue(events),
		QueuePaused:      replayQueuePaused(events),
		LatestResponseID: replayResponses(events).latest,
	}
	selected := map[string]bool{}
	for _, item := range messages[start:end] {
		if projection.PageFromSeq == 0 {
			projection.PageFromSeq = item.seq
		}
		projection.PageThroughSeq = item.seq
		projection.Messages = append(projection.Messages, item.message)
		selected[item.message.ID] = true
	}
	for _, link := range links {
		if selected[link.MessageID] {
			projection.TaskLinks = append(projection.TaskLinks, link)
		}
	}
	// hasMore is retained as a compatibility alias for older clients.
	projection.HasMore = projection.HasPrevious
	return projection, nil
}

// SnapshotThrough returns the complete durable projection up to and including
// throughSeq. It is used to seal assistant input: later conversation events
// must never leak into an already approved task.
func (s *Store) SnapshotThrough(throughSeq uint64) (Projection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, events, err := s.loadLocked()
	if err != nil {
		return Projection{}, err
	}
	if throughSeq > meta.LatestSeq {
		return Projection{}, errors.New("conversation cutoff exceeds latest sequence")
	}
	cutoff := sort.Search(len(events), func(i int) bool { return events[i].Seq > throughSeq })
	hidden := hiddenTeacherMessages(events[:cutoff])
	projection := Projection{ConversationID: meta.ID, UnitID: meta.UnitID, LatestSeq: throughSeq, PageThroughSeq: throughSeq, Messages: []Message{}, TaskLinks: []TaskLink{}}
	for _, event := range events {
		if event.Seq > throughSeq {
			break
		}
		switch event.Type {
		case "message-recorded":
			var message Message
			if json.Unmarshal(event.Data, &message) == nil && !(message.Role == "teacher" && hidden[message.ID]) {
				projection.Messages = append(projection.Messages, message)
			}
		case "task-linked":
			var link TaskLink
			if json.Unmarshal(event.Data, &link) == nil {
				projection.TaskLinks = append(projection.TaskLinks, link)
			}
		}
	}
	return projection, nil
}

func (s *Store) AllMessages() ([]Message, error) {
	sequenced, err := s.SequencedMessages()
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(sequenced))
	for _, item := range sequenced {
		messages = append(messages, item.Message)
	}
	return messages, nil
}

func (s *Store) SequencedMessages() ([]SequencedMessage, error) {
	return s.sequencedMessages(nil)
}

// SequencedMessagesThrough seals both visibility and message order at the
// cutoff. Later response supersession must not change a queued summary input.
func (s *Store) SequencedMessagesThrough(throughSeq uint64) ([]SequencedMessage, error) {
	return s.sequencedMessages(&throughSeq)
}

func (s *Store) sequencedMessages(throughSeq *uint64) ([]SequencedMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, events, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	if throughSeq != nil {
		if *throughSeq > meta.LatestSeq {
			return nil, errors.New("conversation cutoff exceeds latest sequence")
		}
		cutoff := sort.Search(len(events), func(i int) bool { return events[i].Seq > *throughSeq })
		events = events[:cutoff]
	}
	hidden := hiddenTeacherMessages(events)
	var messages []SequencedMessage
	for _, event := range events {
		if event.Type != "message-recorded" {
			continue
		}
		var message Message
		if json.Unmarshal(event.Data, &message) == nil && !(message.Role == "teacher" && hidden[message.ID]) {
			messages = append(messages, SequencedMessage{Seq: event.Seq, EventID: event.EventID, Message: message})
		}
	}
	return messages, nil
}

func (s *Store) FindMessage(id string) (Message, bool, error) {
	msgs, err := s.AllMessages()
	if err != nil {
		return Message{}, false, err
	}
	for _, msg := range msgs {
		if msg.ID == id {
			return msg, true, nil
		}
	}
	return Message{}, false, nil
}

// ResponseForLearner resolves the durable provider response associated with a
// learner message. It enables HTTP idempotency without replaying a provider
// request after a client reconnect.
func (s *Store) ResponseForLearner(learnerMessageID string) (ResponseRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return ResponseRecord{}, false, err
	}
	record, _ := replayResponses(events).byLearner[learnerMessageID]
	return record, record.ResponseID != "", nil
}

// replayResponses folds the event log into response records, queue state, and
// supersession. It is the single source for every projection that must hide
// superseded teacher messages or expose the live queue.
type conversationState struct {
	byLearner map[string]ResponseRecord
	byID      map[string]ResponseInfo
	latest    string // newest non-superseded response id
	latestAll string // newest response id including superseded ones
	queue     []QueueItem
}

type ResponseInfo struct {
	ResponseID                 string
	TeacherMessageID           string
	TriggeringLearnerMessageID string
	Active                     bool
	Superseded                 bool
	Message                    *Message
}

func replayResponses(events []Event) conversationState {
	state := conversationState{byLearner: map[string]ResponseRecord{}, byID: map[string]ResponseInfo{}}
	current := map[string]*ResponseInfo{}
	var order []string
	for _, event := range events {
		switch event.Type {
		case "teacher-response-started":
			var started struct {
				ResponseID                 string `json:"responseId"`
				TeacherMessageID           string `json:"teacherMessageId"`
				TriggeringLearnerMessageID string `json:"triggeringLearnerMessageId"`
			}
			if json.Unmarshal(event.Data, &started) != nil || started.ResponseID == "" {
				continue
			}
			current[started.ResponseID] = &ResponseInfo{ResponseID: started.ResponseID, TeacherMessageID: started.TeacherMessageID, TriggeringLearnerMessageID: started.TriggeringLearnerMessageID, Active: true}
			order = append(order, started.ResponseID)
			state.latestAll = started.ResponseID
		case "message-recorded":
			var message Message
			if json.Unmarshal(event.Data, &message) != nil {
				continue
			}
			for _, response := range current {
				if response.TeacherMessageID == message.ID {
					copyMsg := message
					response.Message = &copyMsg
				}
			}
		case "teacher-response-finished":
			var finished struct {
				ResponseID string `json:"responseId"`
			}
			if json.Unmarshal(event.Data, &finished) == nil {
				if response, ok := current[finished.ResponseID]; ok {
					response.Active = false
				}
			}
		case "response-superseded":
			var superseded struct {
				ResponseID string `json:"responseId"`
			}
			if json.Unmarshal(event.Data, &superseded) == nil {
				if response, ok := current[superseded.ResponseID]; ok {
					response.Superseded = true
				}
			}
		}
	}
	for _, id := range order {
		snapshot := *current[id]
		state.byID[id] = snapshot
		if snapshot.Superseded {
			continue
		}
		state.latest = id
		if snapshot.TriggeringLearnerMessageID != "" {
			state.byLearner[snapshot.TriggeringLearnerMessageID] = ResponseRecord{ResponseID: snapshot.ResponseID, TeacherMessageID: snapshot.TeacherMessageID, Active: snapshot.Active, Message: snapshot.Message}
		}
	}
	return state
}

// replayQueue folds queue events in order. Pointers allow in-place removal.
func replayQueue(events []Event) []QueueItem {
	var items []*QueueItem
	index := map[string]int{}
	for _, event := range events {
		switch event.Type {
		case "learner-queued":
			var item QueueItem
			if json.Unmarshal(event.Data, &item) != nil || item.QueueID == "" {
				continue
			}
			if _, exists := index[item.QueueID]; exists {
				continue
			}
			index[item.QueueID] = len(items)
			items = append(items, &item)
		case "queue-item-edited":
			var patch struct {
				QueueID        string   `json:"queueId"`
				Content        string   `json:"content"`
				AttachmentRefs []string `json:"attachmentRefs"`
			}
			if json.Unmarshal(event.Data, &patch) != nil {
				continue
			}
			if at, ok := index[patch.QueueID]; ok {
				items[at].Content = patch.Content
				items[at].AttachmentRefs = patch.AttachmentRefs
			}
		case "queue-item-discarded", "queue-item-promoted":
			var ref struct {
				QueueID string `json:"queueId"`
			}
			if json.Unmarshal(event.Data, &ref) != nil {
				continue
			}
			if at, ok := index[ref.QueueID]; ok {
				items[at] = nil
				delete(index, ref.QueueID)
			}
		}
	}
	live := make([]QueueItem, 0, len(items))
	for _, item := range items {
		if item != nil {
			live = append(live, *item)
		}
	}
	return live
}

// hiddenTeacherMessages returns teacher message ids whose response was
// superseded; projections must not show them.
func hiddenTeacherMessages(events []Event) map[string]bool {
	startedBy := map[string]string{}
	superseded := map[string]bool{}
	for _, event := range events {
		switch event.Type {
		case "teacher-response-started":
			var started struct {
				ResponseID       string `json:"responseId"`
				TeacherMessageID string `json:"teacherMessageId"`
			}
			if json.Unmarshal(event.Data, &started) == nil {
				startedBy[started.ResponseID] = started.TeacherMessageID
			}
		case "response-superseded":
			var mark struct {
				ResponseID string `json:"responseId"`
			}
			if json.Unmarshal(event.Data, &mark) == nil {
				superseded[mark.ResponseID] = true
			}
		}
	}
	hidden := map[string]bool{}
	for responseID, messageID := range startedBy {
		if superseded[responseID] && messageID != "" {
			hidden[messageID] = true
		}
	}
	return hidden
}

var ErrQueueItemNotFound = errors.New("queue item not found")

func (s *Store) Enqueue(content string, attachmentRefs []string, operationID, providerID string, mediaIDs ...[]string) (QueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return QueueItem{}, err
	}
	if operationID != "" {
		for _, event := range events {
			if event.Type != "learner-queued" {
				continue
			}
			var item QueueItem
			if json.Unmarshal(event.Data, &item) == nil && item.OperationID == operationID {
				return item, nil
			}
		}
	}
	item := QueueItem{QueueID: idgen.New("q"), Content: content, AttachmentRefs: attachmentRefs, OperationID: operationID, ProviderID: providerID, QueuedAt: time.Now().UTC()}
	if len(mediaIDs) > 0 {
		item.MediaBlocks, err = s.attachmentBlocksLocked(mediaIDs[0])
		if err != nil {
			return QueueItem{}, err
		}
	}
	if _, err := s.appendLocked("learner-queued", item); err != nil {
		return QueueItem{}, err
	}
	return item, nil
}

func (s *Store) EditQueueItem(queueID, content string, attachmentRefs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return err
	}
	for _, item := range replayQueue(events) {
		if item.QueueID == queueID {
			_, err := s.appendLocked("queue-item-edited", struct {
				QueueID        string   `json:"queueId"`
				Content        string   `json:"content"`
				AttachmentRefs []string `json:"attachmentRefs"`
			}{queueID, content, attachmentRefs})
			return err
		}
	}
	return ErrQueueItemNotFound
}

func (s *Store) DiscardQueueItem(queueID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return err
	}
	for _, item := range replayQueue(events) {
		if item.QueueID == queueID {
			_, err := s.appendLocked("queue-item-discarded", struct {
				QueueID string `json:"queueId"`
			}{queueID})
			return err
		}
	}
	return ErrQueueItemNotFound
}

func (s *Store) QueueItems() ([]QueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	return replayQueue(events), nil
}

// PromoteQueueItem atomically converts one queued item into a recorded learner
// message plus a promotion event. mode documents why it left the queue.
func (s *Store) PromoteQueueItem(queueID, mode string) (PromotedTurn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return PromotedTurn{}, err
	}
	for _, item := range replayQueue(events) {
		if item.QueueID == queueID {
			return s.promoteLocked(item, mode)
		}
	}
	return PromotedTurn{}, ErrQueueItemNotFound
}

// PromoteQueueHead promotes the oldest queued item, if any.
func (s *Store) PromoteQueueHead(mode string) (PromotedTurn, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return PromotedTurn{}, false, err
	}
	queue := replayQueue(events)
	if len(queue) == 0 {
		return PromotedTurn{}, false, nil
	}
	promoted, err := s.promoteLocked(queue[0], mode)
	return promoted, err == nil, err
}

func (s *Store) promoteLocked(item QueueItem, mode string) (PromotedTurn, error) {
	blocks := []Block{{Type: "markdown", Source: item.Content}}
	blocks = append(blocks, item.MediaBlocks...)
	for _, ref := range item.AttachmentRefs {
		blocks = append(blocks, Block{Type: "attachment", ArtifactRef: ref})
	}
	learner := Message{ID: idgen.New("msg"), Role: "learner", Status: "completed", Blocks: blocks, OperationID: item.OperationID, CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC()}
	for i := range learner.Blocks {
		if learner.Blocks[i].ID == "" {
			learner.Blocks[i].ID = idgen.New("blk")
		}
	}
	e, err := s.appendLocked("message-recorded", learner)
	if err != nil {
		return PromotedTurn{}, err
	}
	if _, err := s.appendLocked("queue-item-promoted", struct {
		QueueID          string `json:"queueId"`
		LearnerMessageID string `json:"learnerMessageId"`
		Mode             string `json:"mode"`
	}{item.QueueID, learner.ID, mode}); err != nil {
		return PromotedTurn{}, err
	}
	return PromotedTurn{Item: item, Learner: learner, LearnerSeq: e.Seq, Mode: mode}, nil
}

// SupersedeResponse hides an old response after regeneration replaced it.
func (s *Store) SupersedeResponse(oldResponseID, newResponseID string) error {
	_, err := s.Append("response-superseded", struct {
		ResponseID    string `json:"responseId"`
		NewResponseID string `json:"newResponseId"`
	}{oldResponseID, newResponseID})
	return err
}

// ResponseByID resolves one response record including supersession state.
func (s *Store) ResponseByID(responseID string) (ResponseInfo, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return ResponseInfo{}, false, err
	}
	info, ok := replayResponses(events).byID[responseID]
	return info, ok, nil
}

// LatestResponseID returns the newest non-superseded response, if any.
func (s *Store) LatestResponseID() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return "", false, err
	}
	state := replayResponses(events)
	return state.latest, state.latest != "", nil
}

// ReconcileInterruptedResponses closes response-start records left active by a
// prior server process. Call it only during startup, before accepting turns;
// calling it while a provider is streaming would incorrectly interrupt work.
func (s *Store) ReconcileInterruptedResponses() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return 0, err
	}
	type activeResponse struct {
		ResponseID       string
		TeacherMessageID string
		Message          *Message
		StartSeq         uint64
	}
	active := map[string]*activeResponse{}
	for _, event := range events {
		switch event.Type {
		case "teacher-response-started":
			var started struct {
				ResponseID       string `json:"responseId"`
				TeacherMessageID string `json:"teacherMessageId"`
			}
			if json.Unmarshal(event.Data, &started) == nil && started.ResponseID != "" {
				active[started.ResponseID] = &activeResponse{ResponseID: started.ResponseID, TeacherMessageID: started.TeacherMessageID, StartSeq: event.Seq}
			}
		case "message-recorded":
			var message Message
			if json.Unmarshal(event.Data, &message) == nil {
				for _, response := range active {
					if response.TeacherMessageID == message.ID {
						copy := message
						response.Message = &copy
					}
				}
			}
		case "teacher-response-finished":
			var finished struct {
				ResponseID string `json:"responseId"`
			}
			if json.Unmarshal(event.Data, &finished) == nil {
				delete(active, finished.ResponseID)
			}
		}
	}
	ordered := make([]*activeResponse, 0, len(active))
	for _, response := range active {
		ordered = append(ordered, response)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].StartSeq < ordered[j].StartSeq })
	for _, response := range ordered {
		status := "interrupted"
		if response.Message == nil {
			now := time.Now().UTC()
			message := Message{ID: response.TeacherMessageID, Role: "teacher", Status: status, Blocks: []Block{}, CreatedAt: now, CompletedAt: now}
			if _, err := s.appendLocked("message-recorded", message); err != nil {
				return 0, err
			}
		} else if response.Message.Status != "" {
			status = response.Message.Status
			if err := s.ensureSourceEvidenceLocked(response.ResponseID, *response.Message, events); err != nil {
				return 0, err
			}
		}
		if _, err := s.appendLocked("teacher-response-finished", map[string]any{"responseId": response.ResponseID, "messageId": response.TeacherMessageID, "status": status, "recoveredAtStartup": true}); err != nil {
			return 0, err
		}
	}
	return len(ordered), nil
}

func ReconcileAllInterruptedResponses() {
	projects, err := workspace.IndexAll()
	if err != nil {
		return
	}
	for _, project := range projects {
		if project.ProjectType != workspace.ProjectTypeSystemLearning {
			continue
		}
		if store, err := New(project.Slug); err == nil {
			_, _ = store.ReconcileInterruptedResponses()
		}
	}
}

func (s *Store) loadLocked() (Meta, []Event, error) {
	var meta Meta
	if err := readJSON(filepath.Join(s.root, "conversation", "conversation.json"), &meta); err != nil {
		return Meta{}, nil, err
	}
	events, err := readEvents(filepath.Join(s.root, "conversation", "events.jsonl"))
	if err != nil {
		return Meta{}, nil, err
	}
	if len(events) > 0 {
		last := events[len(events)-1]
		if meta.LatestSeq != last.Seq || meta.EventCount != uint64(len(events)) {
			meta.LatestSeq = last.Seq
			meta.LatestEventID = last.EventID
			meta.EventCount = uint64(len(events))
			meta.UpdatedAt = last.OccurredAt
			if err := writeJSON(filepath.Join(s.root, "conversation", "conversation.json"), meta); err != nil {
				return Meta{}, nil, err
			}
		}
	}
	return meta, events, nil
}

func readEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	var events []Event
	var seq uint64
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			var event Event
			if err := json.Unmarshal(line, &event); err != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return nil, fmt.Errorf("decode events.jsonl: %w", err)
			}
			if event.Seq != seq+1 {
				return nil, fmt.Errorf("conversation sequence gap at %d", event.Seq)
			}
			seq = event.Seq
			events = append(events, event)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	return events, nil
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(path, append(data, '\n'), 0o644)
}
