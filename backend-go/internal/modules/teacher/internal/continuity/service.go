package continuity

import (
	"context"
	"encoding/json"
	"errors"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	conversation "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
	usage "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/usagestore"
	"github.com/xmz14/lll/backend-go/internal/platform/workspacegate"
	"io"
	"strings"
	"sync"
	"time"
)

// Service runs one bounded background request. Durable state owns the queue;
// notifications only wake the worker and can safely be coalesced.
type Service struct {
	Gateway ai.Gateway
	mu      sync.Mutex
	pending map[string]bool
	deleted map[string]bool
	active  map[string]context.CancelFunc
	notify  chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	OnError func(string, error)
}

func New(g ai.Gateway) *Service {
	if g == nil {
		g = ai.Configured{}
	}
	return &Service{Gateway: g, pending: map[string]bool{}, deleted: map[string]bool{}, active: map[string]context.CancelFunc{}, notify: make(chan struct{}, 1)}
}
func (s *Service) Start(ctx context.Context) error {
	if s.cancel != nil {
		return errors.New("summary service already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	index, e := projects.IndexAll()
	if e != nil {
		return e
	}
	for _, p := range index {
		if p.ProjectType != projects.ProjectTypeSystemLearning {
			continue
		}
		mu := lockFor(p.Slug)
		mu.Lock()
		st, err := load(p.Slug)
		if err == nil {
			changed := false
			for i := range st.Summaries {
				v := &st.Summaries[i]
				if v.Status == "running" {
					v.Status = "interrupted"
					v.Error = "application stopped during summary generation"
					changed = true
				}
				if v.Status == "pending" {
					s.pending[p.Slug] = true
				}
			}
			if changed {
				err = save(p.Slug, st)
			}
		}
		mu.Unlock()
		if err != nil {
			s.report(p.Slug, err)
		}
	}
	s.wg.Add(1)
	go s.loop()
	s.wake()
	return nil
}
func (s *Service) report(slug string, e error) {
	if s.OnError != nil {
		s.OnError(slug, e)
	}
}
func (s *Service) wake() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}
func (s *Service) Notify(slug string) {
	s.mu.Lock()
	if !s.deleted[slug] {
		s.pending[slug] = true
	}
	s.mu.Unlock()
	s.wake()
}
func (s *Service) Close() error {
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
	}
	return nil
}
func (s *Service) ReviveProject(slug string) { s.mu.Lock(); delete(s.deleted, slug); s.mu.Unlock() }
func (s *Service) DeleteProject(slug string, remove func(string) error) error {
	s.mu.Lock()
	s.deleted[slug] = true
	delete(s.pending, slug)
	if cancel := s.active[slug]; cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	mu := lockFor(slug)
	mu.Lock()
	defer mu.Unlock()
	e := remove(slug)
	if e != nil {
		s.ReviveProject(slug)
	}
	return e
}
func (s *Service) Retry(slug, id string) error {
	if e := Retry(slug, id); e != nil {
		return e
	}
	s.Notify(slug)
	return nil
}
func (s *Service) loop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.notify:
		}
		for {
			s.mu.Lock()
			slug := ""
			for key := range s.pending {
				slug = key
				delete(s.pending, key)
				break
			}
			s.mu.Unlock()
			if slug == "" {
				break
			}
			if e := s.process(slug); e != nil {
				s.report(slug, e)
			}
			if s.ctx.Err() != nil {
				return
			}
		}
	}
}
func (s *Service) process(slug string) error {
	leave, e := workspacegate.Enter()
	if e != nil {
		return e
	}
	defer leave()
	mu := lockFor(slug)
	mu.Lock()
	st, e := load(slug)
	if e != nil {
		mu.Unlock()
		return e
	}
	var job *Summary
	for i := range st.Summaries {
		v := &st.Summaries[i]
		if v.Status != "pending" {
			continue
		}
		if !valid(slug, *v) {
			v.Status = "superseded"
			continue
		}
		job = v
		break
	}
	if job == nil {
		e = save(slug, st)
		mu.Unlock()
		return e
	}
	job.Status = "running"
	frozen := *job
	e = save(slug, st)
	mu.Unlock()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ai.WithBackgroundRequest(s.ctx), 30*time.Second)
	defer cancel()
	s.mu.Lock()
	if s.deleted[slug] {
		s.mu.Unlock()
		return nil
	}
	s.active[slug] = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.active, slug); s.mu.Unlock() }()
	data, e := json.Marshal(frozen.Input)
	var output strings.Builder
	reported := map[string]int{}
	if e == nil {
		e = prepareSummary(ctx, slug, &frozen)
	}
	if e == nil {
		e = s.Gateway.Stream(ctx, ai.GatewayRequest{
			BindingName: "section-summary", ProviderID: frozen.ProviderID, Model: frozen.Model, System: frozen.Prompt,
			Prompt:   frozen.PromptReference,
			Messages: []ai.GatewayMessage{{Role: "user", Content: string(data)}},
		}, func(ev ai.GatewayEvent) {
			if ev.Type == "text-delta" {
				if output.Len()+len(ev.Delta) > 64<<10 {
					cancel()
					return
				}
				output.WriteString(ev.Delta)
			}
			if ev.Type == "usage" {
				for k, v := range ev.Usage {
					reported[k] = v
				}
			}
		})
	}
	if e == nil {
		e = ctx.Err()
	}
	result, parseErr := parseSummary(output.String())
	if e == nil {
		e = parseErr
	}
	// Serialize deletion and publication; no network operation holds this lock.
	mu.Lock()
	defer mu.Unlock()
	s.mu.Lock()
	deleted := s.deleted[slug]
	s.mu.Unlock()
	if deleted {
		return nil
	}
	current, readErr := load(slug)
	if readErr != nil {
		return readErr
	}
	for i := range current.Summaries {
		v := &current.Summaries[i]
		if v.ID != frozen.ID || v.Status != "running" {
			continue
		}
		if !valid(slug, *v) {
			v.Status = "superseded"
		} else if e != nil {
			v.Status = "failed"
			if s.ctx.Err() != nil {
				v.Status = "interrupted"
			}
			v.Error = e.Error()
		} else {
			v.Status = "completed"
			v.Concepts = result.Concepts
			v.Scenario = result.Scenario
			now := time.Now().UTC()
			v.CompletedAt = &now
		}
		break
	}
	if err := save(slug, current); err != nil {
		return err
	}
	if c, err := conversation.New(slug); err == nil {
		if meta, err := c.Meta(); err == nil {
			err = usage.AppendNote(slug, usage.Record{ConversationID: meta.ID, UnitID: meta.UnitID, ResponseID: frozen.ID, ProviderID: frozen.ProviderID, InputTokens: reported["inputTokens"], OutputTokens: reported["outputTokens"], UsageReported: len(reported) > 0})
			if err != nil {
				s.report(slug, err)
			}
		}
	}
	s.Notify(slug)
	return e
}

func prepareSummary(ctx context.Context, slug string, job *Summary) error {
	cfg, err := ai.LoadConfig()
	if err != nil {
		return err
	}
	requested := job.ProviderID
	if requested == "" {
		requested = job.SourceProviderID
	}
	managedSource := ai.IsManagedProvider(requested)
	allManaged := false
	if managedSource {
		c, err := conversation.New(slug)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(job.Input))
		for _, message := range job.Input {
			ids = append(ids, message.ID)
		}
		allManaged, err = c.MessagesUseProviderThrough(ids, job.ThroughSeq, ai.IsManagedProvider)
		if err != nil {
			return err
		}
	}
	provider := summaryProvider(cfg, job.ProviderID, requested, allManaged)
	if provider == nil {
		return errors.New("section summary provider is unavailable")
	}
	if job.ProviderID == "" {
		job.ProviderID, job.Model = provider.ID, provider.Model
	}
	if job.Prompt == "" {
		const scene = "auxiliary/section-summary"
		promptCtx := ai.CloudPromptContext(ctx, scene)
		composed, err := ai.ComposePrompt(promptCtx, scene, nil, nil)
		if err != nil {
			return err
		}
		if len(composed.Tools) != 0 {
			return errors.New("summary generation cannot enable tools")
		}
		job.Prompt, job.PromptHash = composed.Text, hash(composed.Text)
		job.PromptReference = ai.ManagedPromptReference(promptCtx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return update(slug, job.ID, func(current *Summary) error {
		if current.Status != "running" || !valid(slug, *current) {
			return errors.New("summary source changed before inference")
		}
		current.ProviderID, current.Model = job.ProviderID, job.Model
		current.Prompt, current.PromptHash = job.Prompt, job.PromptHash
		current.PromptReference = job.PromptReference
		return nil
	})
}

func summaryProvider(cfg *ai.Config, frozen, requested string, allManaged bool) *ai.ProviderConfig {
	if cfg == nil {
		return nil
	}
	if frozen != "" {
		if ai.IsManagedProvider(frozen) && !allManaged {
			return nil
		}
		return cfg.FindExact(frozen)
	}
	for _, name := range []string{"section-summary", "notes"} {
		if binding := cfg.Bindings[name]; binding.ProviderID != "" {
			return cfg.ResolveDirect(name)
		}
	}
	if requested != "" && (!ai.IsManagedProvider(requested) || allManaged) {
		return cfg.FindExact(requested)
	}
	return cfg.ResolveDirect("teacher")
}

type summaryOutput struct {
	Concepts []string `json:"concepts"`
	Scenario string   `json:"scenario,omitempty"`
}

func parseSummary(raw string) (summaryOutput, error) {
	var out summaryOutput
	d := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	d.DisallowUnknownFields()
	if e := d.Decode(&out); e != nil {
		return out, e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return out, errors.New("extra summary output")
	}
	if out.Concepts == nil {
		return out, errors.New("missing concepts")
	}
	seen := map[string]bool{}
	clean := []string{}
	for _, v := range out.Concepts {
		v = strings.TrimSpace(v)
		if v == "" {
			return out, errors.New("empty concept")
		}
		if !seen[v] {
			seen[v] = true
			clean = append(clean, v)
		}
	}
	out.Concepts = clean
	out.Scenario = strings.TrimSpace(out.Scenario)
	return out, nil
}
