package noteservice

import (
	"context"
	"errors"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	projects "github.com/xmz14/lll/backend-go/internal/modules/projects"

	conversation "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/notestore"
	config "github.com/xmz14/lll/backend-go/internal/platform/config"
	"sync"
	"time"
)

var ErrProjectBusy = errors.New("project has active note generation")

type Service struct {
	deleted                 sync.Map
	initializationFailed    map[string]bool
	OnInitializationFailure func(string, error)
	generator               notestore.NoteGenerator
	OnUpdated               func(string, string, string, string)
	mu                      sync.Mutex
	settingsMu              sync.Mutex
	pending                 map[string]bool
	busy                    map[string]bool
	notify                  chan struct{}
	ctx                     context.Context
	cancel                  context.CancelFunc
	wg                      sync.WaitGroup
}

func New(generator notestore.NoteGenerator) *Service {
	if generator == nil {
		generator = Generate
	}
	return &Service{initializationFailed: map[string]bool{}, generator: generator, pending: map[string]bool{}, busy: map[string]bool{}, notify: make(chan struct{}, 1)}
}
func (s *Service) Start(ctx context.Context) error {
	if s.cancel != nil {
		return errors.New("notes service already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	index, err := projects.IndexAll()
	if err != nil {
		return err
	}
	for _, project := range index {
		if project.ProjectType != projects.ProjectTypeSystemLearning || s.blocked(project.Slug) {
			continue
		}

		err := s.initializeProject(project.Slug)
		if err != nil {
			s.initializationFailed[project.Slug] = true
			if s.OnInitializationFailure != nil {
				s.OnInitializationFailure(project.Slug, err)
			}
			continue
		}

		if err := s.ObserveCompleted(project.Slug); err != nil {
			s.initializationFailed[project.Slug] = true
			if s.OnInitializationFailure != nil {
				s.OnInitializationFailure(project.Slug, err)
			}
		}
	}
	s.wg.Add(1)
	go s.loop()
	return nil
}
func (s *Service) blocked(slug string) bool {
	_, deleted := s.deleted.Load(slug)
	return deleted || s.initializationFailed[slug]
}
func (s *Service) Close() error {
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
	}
	return nil
}
func (s *Service) Notify(slug string) {
	if s.blocked(slug) {
		return
	}
	s.mu.Lock()
	s.pending[slug] = true
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}
func (s *Service) observe(slug string, cfg notestore.AutoNotesSettings) error {
	if s.blocked(slug) {
		return errors.New("project unavailable")
	}
	c, err := conversation.New(slug)
	if err != nil {
		return err
	}
	meta, err := c.Meta()
	if err != nil {
		return err
	}
	store, err := notestore.New(slug)
	if err != nil {
		return err
	}
	if err := store.Initialize(meta.LatestSeq); err != nil {
		return err
	}
	turns, err := c.SuccessfulNoteTurns(0)
	if err != nil {
		return err
	}
	if cfg.ProviderID == "" {
		if ai, err := ai.LoadConfig(); err == nil && ai != nil {
			if binding, ok := ai.Bindings["notes"]; ok && binding.ProviderID != "" {
				cfg.ProviderID = binding.ProviderID
				if cfg.Model == "" {
					cfg.Model = binding.Model
				}
			} else if teacher := ai.Resolve("teacher"); teacher != nil {
				cfg.ProviderID = teacher.ID
				cfg.Model = teacher.Model
			}
		}
	}
	if cfg.ProviderID != "" && cfg.Model == "" {
		if ai, err := ai.LoadConfig(); err == nil && ai != nil {
			if p := ai.FindExact(cfg.ProviderID); p != nil {
				cfg.Model = p.Model
			}
		}
	}
	jobs, err := store.Observe(cfg, meta.LatestSeq, turns)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		s.updated(slug, job)
	}
	s.Notify(slug)
	return nil
}
func (s *Service) ObserveCompleted(slug string) error {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	cfg, err := config.LoadTeacherSettings()
	if err != nil {
		return err
	}
	return s.observe(slug, cfg)
}
func (s *Service) UpdateSettings(cfg notestore.AutoNotesSettings) error {
	if err := config.ValidateAutoNotes(cfg); err != nil {
		return err
	}
	if cfg.ProviderID != "" {
		ai, err := ai.LoadConfig()
		if err != nil {
			return err
		}
		if ai == nil || ai.FindExact(cfg.ProviderID) == nil {
			return errors.New("note provider not found")
		}
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	old, err := config.LoadTeacherSettings()
	if err != nil {
		return err
	}
	index, err := projects.IndexAll()
	if err != nil {
		return err
	}
	for _, p := range index {
		if p.ProjectType == projects.ProjectTypeSystemLearning && !s.blocked(p.Slug) {
			if err := s.observe(p.Slug, old); err != nil {
				return err
			}
		}
	}
	if err := config.SaveTeacherSettings(cfg); err != nil {
		return err
	}
	for _, p := range index {
		if p.ProjectType == projects.ProjectTypeSystemLearning && !s.blocked(p.Slug) {
			if err := s.observe(p.Slug, cfg); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) updated(slug string, j notestore.NoteJob) {
	if s.OnUpdated != nil {
		s.OnUpdated(slug, j.ID, j.Status, j.NoteID)
	}
}
func (s *Service) loop() {
	defer s.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.notify:
		case <-ticker.C:
		}
		s.dispatch()
	}
}
func (s *Service) dispatch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for slug := range s.pending {
		if s.blocked(slug) {
			delete(s.pending, slug)
			continue
		}
		if len(s.busy) >= 2 {
			return
		}
		if s.busy[slug] {
			continue
		}
		delete(s.pending, slug)
		store, err := notestore.New(slug)
		if err != nil {
			continue
		}
		resource, err := store.List()
		if err != nil {
			continue
		}
		for _, job := range resource.Jobs {
			if job.Status != "queued" {
				continue
			}
			job, err = store.SetRunning(job.ID)
			if err != nil {
				break
			}
			s.busy[slug] = true
			s.wg.Add(1)
			go s.run(slug, store, job)
			break
		}
	}
}
func (s *Service) run(slug string, store *notestore.Store, job notestore.NoteJob) {
	defer s.wg.Done()
	defer func() { s.mu.Lock(); delete(s.busy, slug); s.mu.Unlock(); s.Notify(slug) }()
	s.updated(slug, job)
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()
	content, usage, err := s.generator(ctx, job.ProviderID, job.Model, job.Turns)
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		if usageErr := recordUsage(slug, job, usage); usageErr != nil {
			if err == nil {
				err = usageErr
			}
		}
	}
	if err != nil {
		code := "generation_failed"
		if errors.Is(err, context.Canceled) {
			code = "interrupted"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			code = "timeout"
		}
		if errors.Is(err, ErrProviderNotConfigured) {
			code = "provider_not_configured"
		}
		_ = store.MarkFailed(job.ID, code)
		job.Status, job.ErrorCode = "failed", code
		s.updated(slug, job)
		return
	}
	entry, err := store.Commit(job.ID, content, job.ProviderID, job.Model, usage)
	if err != nil {
		_ = store.MarkFailed(job.ID, "persist_failed")
		job.Status = "failed"
	} else {
		job.Status, job.NoteID = "succeeded", entry.ID
	}
	s.updated(slug, job)
}

func (s *Service) initializeProject(slug string) error {
	c, err := conversation.New(slug)
	if err != nil {
		return err
	}
	meta, err := c.Meta()
	if err != nil {
		return err
	}
	store, err := notestore.New(slug)
	if err != nil {
		return err
	}
	if err := store.Initialize(meta.LatestSeq); err != nil {
		return err
	}
	return store.Recover()
}

// DeleteProject excludes note observation and dispatch while deleting current data.
func (s *Service) DeleteProject(slug string, remove func(string) error) error {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[slug] {
		return ErrProjectBusy
	}
	if err := remove(slug); err != nil {
		return err
	}
	s.deleted.Store(slug, true)
	delete(s.pending, slug)
	return nil
}
func (s *Service) ReviveProject(slug string) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.deleted.Delete(slug)
}
