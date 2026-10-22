package teacher

import (
	"context"
	"errors"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrResponseActive = errors.New("response_active")
var ErrResponseNotFound = errors.New("teacher response not found")
var ErrNotLatestResponse = errors.New("not_latest_response")
var ErrInvalidTurn = errors.New("invalid teacher turn")
var ErrStopTimeout = errors.New("active teacher response did not stop in time")
var ErrLifecycleClosed = errors.New("teacher lifecycle closed")

// Lifecycle owns durable turn scheduling independently of network connections.
type Lifecycle struct {
	startMu        sync.Mutex
	wg             sync.WaitGroup
	Service        *Service
	Responses      *ActiveResponses
	OnQueueUpdated func(string)
	locks          sync.Map
	closed         atomic.Bool
	deleted        sync.Map
}

func NewLifecycle(service *Service) *Lifecycle {
	if service == nil {
		service = New(nil)
	}
	return &Lifecycle{Service: service, Responses: NewActiveResponses()}
}
func (s *Lifecycle) lock(slug string) *sync.Mutex {
	v, _ := s.locks.LoadOrStore(slug, &sync.Mutex{})
	return v.(*sync.Mutex)
}
func (s *Lifecycle) notify(slug string) {
	if s.OnQueueUpdated != nil {
		s.OnQueueUpdated(slug)
	}
}
func (s *Lifecycle) available(slug string) bool {
	_, deleted := s.deleted.Load(slug)
	return !s.closed.Load() && !deleted
}
func (s *Lifecycle) Turn(slug string, in TurnInput) (*ActiveResponse, error) {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	if !s.available(slug) {
		return nil, ErrLifecycleClosed
	}
	store, err := NewConversation(slug)
	if err != nil {
		return nil, err
	}
	if err = EnsureGreeting(store, slug); err != nil {
		return nil, err
	}
	if _, err = store.AttachmentBlocks(in.ConversationAttachmentIDs); err != nil {
		return nil, ErrInvalidTurn
	}
	run := s.startTeacherRunLocked(slug, func(ctx context.Context, emit func(StreamFrame)) error {
		return s.Service.StreamTurn(ctx, slug, in, emit)
	})
	if run == nil {
		return nil, ErrResponseActive
	}
	return run, nil
}
func (s *Lifecycle) Queue(slug string, in TurnInput) (string, bool, error) {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	if !s.available(slug) {
		return "", false, ErrLifecycleClosed
	}
	content := strings.TrimSpace(in.Content)
	if in.OperationID == "" || (content == "" && len(in.ConversationAttachmentIDs) == 0) || len([]byte(content)) > 64<<10 {
		return "", false, ErrInvalidTurn
	}
	store, err := NewConversation(slug)
	if err != nil {
		return "", false, err
	}
	if err = EnsureGreeting(store, slug); err != nil {
		return "", false, err
	}
	item, err := store.Enqueue(content, in.AttachmentRefs, in.OperationID, in.ProviderID, in.ConversationAttachmentIDs)
	if err != nil {
		return "", false, err
	}
	started := s.advanceQueueLocked(slug)
	promoted := started
	if items, err := store.QueueItems(); err == nil {
		for _, queued := range items {
			if queued.QueueID == item.QueueID {
				promoted = false
			}
		}
	}
	s.notify(slug)
	return item.QueueID, promoted, nil
}
func (s *Lifecycle) EditQueue(slug, id, content string, refs []string) error {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	if !s.available(slug) {
		return ErrLifecycleClosed
	}
	content = strings.TrimSpace(content)
	if content == "" || len([]byte(content)) > 64<<10 {
		return ErrInvalidTurn
	}
	store, err := NewConversation(slug)
	if err != nil {
		return err
	}
	if err = store.EditQueueItem(id, content, refs); err == nil {
		s.notify(slug)
	}
	return err
}
func (s *Lifecycle) DiscardQueue(slug, id string) error {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	if !s.available(slug) {
		return ErrLifecycleClosed
	}
	store, err := NewConversation(slug)
	if err != nil {
		return err
	}
	if err = store.DiscardQueueItem(id); err == nil {
		s.notify(slug)
	}
	return err
}
func (s *Lifecycle) Steer(ctx context.Context, slug, id string) (*ActiveResponse, error) {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	if !s.available(slug) {
		return nil, ErrLifecycleClosed
	}
	store, err := NewConversation(slug)
	if err != nil {
		return nil, err
	}
	if run := s.Responses.Project(slug); run != nil {
		run.Stop()
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-run.Done():
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, ErrStopTimeout
		}
	}
	promoted, err := store.PromoteQueueItem(id, "steer")
	if err != nil {
		return nil, err
	}
	run := s.startTeacherRunLocked(slug, func(ctx context.Context, emit func(StreamFrame)) error {
		return s.Service.StreamPromotedTurn(ctx, slug, promoted, true, emit)
	})
	if run == nil {
		return nil, ErrResponseActive
	}
	s.notify(slug)
	return run, nil
}
func (s *Lifecycle) Regenerate(slug, id, provider string) (*ActiveResponse, error) {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	if !s.available(slug) {
		return nil, ErrLifecycleClosed
	}
	store, err := NewConversation(slug)
	if err != nil {
		return nil, err
	}
	info, found, err := store.ResponseByID(id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrResponseNotFound
	}
	if info.Active {
		return nil, ErrResponseActive
	}
	latest, ok, err := store.LatestResponseID()
	if err != nil {
		return nil, err
	}
	if info.Superseded || !ok || latest != id {
		return nil, ErrNotLatestResponse
	}
	run := s.startTeacherRunLocked(slug, func(ctx context.Context, emit func(StreamFrame)) error {
		return s.Service.RegenerateResponse(ctx, slug, id, provider, emit)
	})
	if run == nil {
		return nil, ErrResponseActive
	}
	return run, nil
}
func (s *Lifecycle) Opening(slug, provider string) (*ActiveResponse, error) {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	if !s.available(slug) {
		return nil, ErrLifecycleClosed
	}
	if run := s.Responses.Project(slug); run != nil {
		return run, nil
	}
	store, err := NewConversation(slug)
	if err != nil {
		return nil, err
	}
	projection, err := store.Read(0, 500)
	if err != nil {
		return nil, err
	}
	for _, m := range projection.Messages {
		if m.OperationID != "teacher-welcome-v1" {
			return nil, nil
		}
	}
	claimed, err := store.ClaimOpening()
	if err != nil || !claimed {
		return nil, err
	}
	return s.startTeacherRunLocked(slug, func(ctx context.Context, emit func(StreamFrame)) error {
		return s.Service.StreamOpening(ctx, slug, provider, emit)
	}), nil
}
func (s *Lifecycle) Stop(slug, id string) error {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	run := s.Responses.Response(id)
	if run == nil || run != s.Responses.Project(slug) {
		return ErrResponseNotFound
	}
	run.Stop()
	return nil
}

// DeleteProject serializes deletion against new turns and queue promotion.
func (s *Lifecycle) DeleteProject(slug string, remove func(string) error) error {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	if s.Responses.Project(slug) != nil {
		return ErrResponseActive
	}
	if err := remove(slug); err != nil {
		return err
	}
	s.deleted.Store(slug, true)
	return nil
}
func (s *Lifecycle) ReviveProject(slug string) {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	s.deleted.Delete(slug)
}
func (s *Lifecycle) Close() { s.closed.Store(true); s.Responses.StopAll() }

// startTeacherRunLocked begins one provider response owned by the server-side
// ActiveResponse registry. Caller must hold the project turn lock. When the
// turn ends it advances the queue, so queued work continues without a client.
func (s *Lifecycle) startTeacherRunLocked(slug string, start func(ctx context.Context, emit func(StreamFrame)) error) *ActiveResponse {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	activeTeacher := s.Responses
	if !s.available(slug) || activeTeacher.Project(slug) != nil {
		return nil
	}
	ctx, run, started := activeTeacher.Start(slug)
	if !started {
		return nil
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		responseID := ""
		emit := func(frame StreamFrame) {
			run.Append(frame)
			if frame.Type == "message-started" {
				s.notify(slug)
			}
			if frame.Type == "turn-accepted" {
				if id, _ := frame.Data["responseId"].(string); id != "" && responseID == "" {
					responseID = id
					activeTeacher.Bind(id, run)
				}
			}
		}
		if err := start(ctx, emit); err != nil {
			emit(StreamFrame{Type: "message-failed", Data: map[string]any{"code": "teacher_provider_unavailable", "partialPreserved": false}})
		}
		activeTeacher.Finish(slug, responseID, run)
		s.advanceTeacherQueue(slug)
		s.notify(slug)
	}()
	return run
}

// advanceTeacherQueue promotes the head of the durable queue when no response
// is active. It is safe to call from any turn completion path.
func (s *Lifecycle) advanceTeacherQueue(slug string) bool {
	lock := s.lock(slug)
	lock.Lock()
	defer lock.Unlock()
	return s.advanceQueueLocked(slug)
}
func (s *Lifecycle) advanceQueueLocked(slug string) bool {
	if !s.available(slug) {
		return false
	}
	if s.Responses.Project(slug) != nil {
		return false
	}
	store, err := NewConversation(slug)
	if err != nil {
		return false
	}
	promoted, ok, err := store.PromoteQueueHead("auto")
	if err != nil || !ok {
		return false
	}
	return s.startTeacherRunLocked(slug, func(ctx context.Context, emit func(StreamFrame)) error {
		return s.Service.StreamPromotedTurn(ctx, slug, promoted, false, emit)
	}) != nil
}

func EnsureGreeting(store *ConversationStore, slug string) error {
	if store == nil {
		return errors.New("teacher conversation is unavailable")
	}
	meta, err := store.Meta()
	if err != nil {
		return err
	}
	if meta.EventCount > 0 {
		return nil
	}
	messages, err := store.AllMessages()
	if err != nil || len(messages) > 0 {
		return err
	}
	state, err := projects.ReadProjectState(slug)
	if err != nil {
		return err
	}
	text := "你好，我是「" + state.Title + "」的教师。你可以直接告诉我：最想先弄懂什么、目前卡在哪里，或者希望我从哪里开始引导？"
	_, _, err = store.AppendMessage("teacher", "completed", "teacher-welcome-v1", []Block{{Type: "markdown", Source: text}})
	return err
}
