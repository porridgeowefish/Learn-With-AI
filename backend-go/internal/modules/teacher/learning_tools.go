package teacher

import (
	"context"
	"errors"
	"github.com/xmz14/lll/backend-go/internal/modules/activity"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/assets"
	"strings"
	"sync"
)

var ErrAIUnavailable = errors.New("ask-ai not configured")
var ErrProviderMissing = errors.New("provider not found")
var ErrReviewEmpty = errors.New("review_empty")

// LearningTools owns Feynman teaching operations.
// HTTP adapters only forward the returned stream and encode domain errors.
type LearningTools struct {
	OnActivity         func(string, activity.ProgressEvent)
	OnFeynmanCompleted func(slug, sessionID, messageID, content string)
	ctx                context.Context
	cancel             context.CancelFunc
	mu                 sync.Mutex
	active             map[string]int
	deleted            map[string]bool
	closed             bool
	wg                 sync.WaitGroup
}

func NewLearningTools() *LearningTools {
	ctx, cancel := context.WithCancel(context.Background())
	return &LearningTools{ctx: ctx, cancel: cancel, active: map[string]int{}, deleted: map[string]bool{}}
}
func (s *LearningTools) begin(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.deleted[slug] {
		return ErrLifecycleClosed
	}
	s.active[slug]++
	s.wg.Add(1)
	return nil
}
func (s *LearningTools) end(slug string) { s.mu.Lock(); s.active[slug]--; s.mu.Unlock(); s.wg.Done() }
func (s *LearningTools) Close()          { s.mu.Lock(); s.closed = true; s.cancel(); s.mu.Unlock(); s.wg.Wait() }
func (s *LearningTools) DeleteProject(slug string, remove func(string) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[slug] > 0 {
		return ErrResponseActive
	}
	if err := remove(slug); err != nil {
		return err
	}
	s.deleted[slug] = true
	return nil
}
func (s *LearningTools) ReviveProject(slug string) {
	s.mu.Lock()
	delete(s.deleted, slug)
	s.mu.Unlock()
}
func (s *LearningTools) activity(slug string, e activity.ProgressEvent) {
	if s.OnActivity != nil {
		s.OnActivity(slug, e)
	}
}
func (s *LearningTools) streamContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}
func resolveToolProvider(id, purpose string) (*ai.ProviderConfig, error) {
	cfg, err := ai.LoadConfig()
	if err != nil || cfg == nil || (!cfg.Enabled() && !ai.IsManagedProvider(id)) {
		return nil, ErrAIUnavailable
	}
	pc := cfg.Find(id)
	if id == "" {
		pc = cfg.Resolve(purpose)
	}
	if pc == nil {
		return nil, ErrProviderMissing
	}
	return pc, nil
}

type ToolStream func(context.Context, func(ai.AIFrame)) error
type ToolInput struct {
	ProviderID string
	Content    string
}

func CreateFeynmanSession(slug string) (assets.FeynmanSession, error) {
	store, err := assets.New(slug)
	if err != nil {
		return assets.FeynmanSession{}, err
	}
	review, err := store.Get("review")
	if err != nil {
		return assets.FeynmanSession{}, err
	}
	if strings.TrimSpace(review.Content) == "" {
		return assets.FeynmanSession{}, ErrReviewEmpty
	}
	return store.CreateFeynmanSession()
}
func (s *LearningTools) PrepareFeynman(slug, id string, in ToolInput) (ToolStream, error) {
	in.Content = strings.TrimSpace(in.Content)
	store, err := assets.New(slug)
	if err != nil {
		return nil, err
	}
	session, err := store.FeynmanSession(id)
	if err != nil {
		return nil, err
	}
	if in.Content == "" && len(session.Messages) > 0 {
		return nil, ErrInvalidTurn
	}
	pc, err := resolveToolProvider(in.ProviderID, "teacher")
	if err != nil {
		return nil, err
	}
	review, _, err := store.ReviewContentForFeynman(session)
	if err != nil {
		return nil, err
	}
	return func(parent context.Context, emit func(ai.AIFrame)) error {
		if err := s.begin(slug); err != nil {
			return err
		}
		defer s.end(slug)
		ctx, cancel := s.streamContext(parent)
		defer cancel()
		messages := make([]ai.AIMessage, 0, len(session.Messages)+1)
		for _, m := range session.Messages {
			role := "user"
			if m.Role == "teacher" {
				role = "assistant"
			}
			messages = append(messages, ai.AIMessage{Role: role, Content: m.Content})
		}
		learnerID := ""
		if in.Content == "" {
			messages = append(messages, ai.AIMessage{Role: "user", Content: FeynmanOpening})
		} else {
			updated, err := store.AppendFeynmanMessage(session.SessionID, assets.FeynmanMessage{Role: "learner", Content: in.Content})
			if err != nil {
				return err
			}
			messages = append(messages, ai.AIMessage{Role: "user", Content: in.Content})
			last := updated.Messages[len(updated.Messages)-1]
			learnerID = last.ID
			s.activity(slug, activity.ProgressEvent{ID: "feynman:" + last.ID, SourceType: "feynman", SourceID: session.SessionID, ActivityDelta: 1, Title: "讲给教师听"})
		}
		var text strings.Builder
		composition, promptErr := BuildFeynmanComposition(ctx, review)
		if promptErr != nil {
			return promptErr
		}
		prefix := make([]ai.AIMessage, 0, len(composition.Messages)+len(messages))
		var references []ai.AIMessage
		for _, message := range composition.Messages {
			converted := ai.AIMessage{Role: message.Role, Content: message.Content}
			if message.Role == "system" {
				prefix = append(prefix, converted)
			} else {
				references = append(references, converted)
			}
		}
		messages = append(append(prefix, messages...), references...)
		err = ai.Stream(ctx, ai.Provider{ID: pc.ID, Kind: pc.Kind, BaseURL: pc.BaseURL, APIKey: pc.APIKey, Model: pc.Model}, "", messages, func(f ai.AIFrame) {
			if f.Type == "thinking" || f.Type == "done" {
				return
			}
			if f.Type == "text" {
				text.WriteString(f.Content)
			}
			emit(f)
		})
		status := "completed"
		if err != nil {
			status = "failed"
			if errors.Is(err, context.Canceled) {
				status = "interrupted"
			}
		}
		if text.Len() > 0 {
			if _, saveErr := store.AppendFeynmanMessage(session.SessionID, assets.FeynmanMessage{Role: "teacher", Content: text.String(), Status: status}); saveErr != nil {
				return saveErr
			}
		}
		if err == nil {
			if learnerID != "" && text.Len() > 0 && s.OnFeynmanCompleted != nil {
				s.OnFeynmanCompleted(slug, session.SessionID, learnerID, in.Content)
			}
			emit(ai.AIFrame{Type: "done"})
		}
		return err
	}, nil
}
