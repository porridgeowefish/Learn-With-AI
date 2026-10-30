package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	sources "github.com/xmz14/lll/backend-go/internal/modules/sources"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	"github.com/xmz14/lll/backend-go/internal/modules/assets"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"

	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
)

type Usage struct {
	RequestID  string         `json:"requestId"`
	JobID      string         `json:"jobId"`
	Attempt    int            `json:"attempt"`
	ProviderID string         `json:"providerId"`
	Model      string         `json:"model"`
	State      string         `json:"state"`
	Tokens     map[string]int `json:"tokens,omitempty"`
	Known      bool           `json:"known"`
	CreatedAt  time.Time      `json:"createdAt"`
}

func appendUsage(root string, u Usage) error {
	path := filepath.Join(root, "generation", "usage.jsonl")
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	b, e := json.Marshal(u)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(append(b, '\n')); e != nil {
		return e
	}
	return f.Sync()
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.stop != nil {
		s.mu.Unlock()
		return nil
	}
	run, cancel := context.WithCancel(ctx)
	s.stop = cancel
	if e := s.recoverLocked(); e != nil {
		s.stop = nil
		cancel()
		s.mu.Unlock()
		return e
	}
	s.mu.Unlock()
	for n := 0; n < 2; n++ {
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-run.Done():
					return
				case <-ticker.C:
				case <-s.notify:
				}
				for {
					slug, j, ok := s.claim(run)
					if !ok {
						break
					}
					s.execute(run, slug, j)
				}
			}
		}()
	}
	s.wake()
	return nil
}
func (s *Service) Close() {
	s.mu.Lock()
	if s.stop != nil {
		s.stop()
	}
	for _, cancel := range s.active {
		cancel()
	}
	s.mu.Unlock()
	s.workers.Wait()
}
func (s *Service) recoverLocked() error {
	list, e := projects.IndexAll()
	if e != nil {
		return e
	}
	for _, p := range list {
		if p.LayoutStatus != "current" {
			continue
		}
		root, e := projects.ProjectRootForSlug(p.Slug)
		if e != nil {
			return e
		}
		jobs, e := loadJobs(root)
		if e != nil {
			return e
		}
		for _, j := range jobs {
			if j.Status != "running" && j.Status != "queued" {
				continue
			}
			if j.Status == "running" {
				refs, e := committedResult(p.Slug, j)
				if e != nil {
					return e
				}
				if len(refs) > 0 {
					j.Status = "succeeded"
					j.ResultRefs = refs
				} else {
					j.Status = "interrupted"
					j.ErrorCode = "request-interrupted"
				}
				j.Phase = "done"
			} else if _, e := frozenProvider(j); e != nil {
				j.Status = "failed"
				j.Phase = "done"
				j.ErrorCode = "provider-unavailable"
			}
			if e = s.saveLocked(p.Slug, root, &j); e != nil {
				return e
			}
		}
	}
	return nil
}
func committedResult(slug string, j Job) ([]string, error) {
	if j.Kind == "discipline-map" {
		return mapResultForJob(slug, j.ID)
	}
	st, e := assets.New(slug)
	if e != nil {
		return nil, e
	}
	vs, e := st.Versions(j.Kind)
	if e != nil {
		return nil, e
	}
	current, e := st.Get(j.Kind)
	if e != nil {
		return nil, e
	}
	lineage := map[string]bool{}
	parent := current.Meta.CurrentVersionID
	for parent != "" {
		if lineage[parent] {
			break
		}
		lineage[parent] = true
		next := ""
		for _, v := range vs {
			if v.VersionID == parent {
				next = v.ParentVersionID
				break
			}
		}
		parent = next
	}
	for _, v := range vs {
		if v.TaskID == j.ID && v.RunID == j.ID && lineage[v.VersionID] {
			return []string{v.VersionID}, nil
		}
	}
	return nil, nil
}
func frozenProvider(j Job) (ai.ProviderConfig, error) {
	cfg, e := ai.LoadConfig()
	if e != nil || cfg == nil {
		return ai.ProviderConfig{}, problem("provider-unavailable", 409)
	}
	p := cfg.FindExact(j.ProviderID)
	if p == nil || (p.Kind != "siwc" && (p.APIKey == "" || p.BaseURL == "")) || (p.Kind != "openai" && p.Kind != "anthropic" && p.Kind != "siwc") {
		return ai.ProviderConfig{}, problem("provider-unavailable", 409)
	}
	copy := *p
	copy.Model = j.Model
	return copy, nil
}
func (s *Service) claim(ctx context.Context) (string, Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return "", Job{}, false
	}
	list, e := projects.IndexAll()
	if e != nil {
		return "", Job{}, false
	}
	var oldest Job
	selectedSlug, selectedRoot := "", ""
	for _, p := range list {
		if p.LayoutStatus != "current" {
			continue
		}
		if s.deleted[p.Slug] {
			continue
		}
		root, e := projects.ProjectRootForSlug(p.Slug)
		if e != nil {
			continue
		}
		jobs, e := loadJobs(root)
		if e != nil {
			continue
		}
		for i := len(jobs) - 1; i >= 0; i-- {
			j := jobs[i]
			if j.Status != "queued" {
				continue
			}
			if selectedSlug == "" || j.CreatedAt.Before(oldest.CreatedAt) || (j.CreatedAt.Equal(oldest.CreatedAt) && j.ID < oldest.ID) {
				oldest, selectedSlug, selectedRoot = j, p.Slug, root
			}
		}
	}
	if selectedSlug != "" {
		oldest.Status, oldest.Phase = "running", "preparing"
		if s.saveLocked(selectedSlug, selectedRoot, &oldest) == nil {
			return selectedSlug, oldest, true
		}
	}
	return "", Job{}, false
}
func (s *Service) phase(slug string, j *Job, phase string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, e := getJob(slug, j.ID)
	if e != nil || current.Status != "running" || s.deleted[slug] {
		return false
	}
	j.Phase = phase
	root, e := projects.ProjectRootForSlug(slug)
	return e == nil && s.saveLocked(slug, root, j) == nil
}
func (s *Service) execute(parent context.Context, slug string, j Job) {
	ctx, cancel := context.WithTimeout(parent, 100*time.Minute)
	defer cancel()
	s.mu.Lock()
	current, e := getJob(slug, j.ID)
	if e != nil || current.Status != "running" || s.deleted[slug] {
		s.mu.Unlock()
		return
	}
	s.active[j.ID] = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.active, j.ID); s.mu.Unlock() }()
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return
	}
	var f frozenInput
	if e = readJSON(inputPath(root, j.ID), &f); e != nil {
		s.fail(slug, j, "input-unavailable")
		return
	}
	p, e := frozenProvider(j)
	if e != nil {
		s.fail(slug, j, "provider-unavailable")
		return
	}
	outputBudget := ai.GenerationOutputTokens(p)
	thinkingBudget := f.MaxThinkingTokens
	if thinkingBudget == 0 {
		thinkingBudget = ai.GenerationThinkingTokens(p)
	}
	req := ai.GatewayRequest{System: f.System, Messages: f.Messages, ProviderID: j.ProviderID, Model: j.Model, MaxOutputTokens: outputBudget, MaxThinkingTokens: thinkingBudget}
	if len(f.Sources) > 0 {
		req.Tools = sources.Tools()
	}
	var searcher teacher.WebSearcher
	if f.WebSearchEnabled {
		searcher = teacher.ConfiguredWebSearch()
		if searcher != nil {
			req.Tools = append(req.Tools, webSearchTool())
		}
	}
	var reader *sources.Reader
	if len(f.Sources) > 0 {
		st, e := sources.New(slug)
		if e != nil {
			s.fail(slug, j, "source-unavailable")
			return
		}
		reader = sources.NewReader(st)
	}
	if !s.phase(slug, &j, "generating") {
		return
	}
	text := ""
	sourceCalls := 0
	sourceTokens := 0
	webCalls := 0
	completedTokens := 0
	completedTokensEstimated := false
	lastProgress := time.Time{}
	for round := 0; round < 8; round++ {
		available, e := ai.CheckGenerationRequest(req, p)
		if e != nil {
			s.fail(slug, j, "input-budget-exhausted")
			return
		}
		requestID := idgen.New("greq")
		s.mu.Lock()
		cur, e := getJob(slug, j.ID)
		if e != nil || cur.Status != "running" || s.deleted[slug] {
			s.mu.Unlock()
			return
		}
		j.RequestIDs = append(j.RequestIDs, requestID)
		e = s.saveLocked(slug, root, &j)
		if e == nil {
			e = appendUsage(root, Usage{RequestID: requestID, JobID: j.ID, Attempt: j.Attempt, ProviderID: j.ProviderID, Model: j.Model, State: "request-started", CreatedAt: time.Now().UTC()})
		}
		s.mu.Unlock()
		if e != nil {
			s.fail(slug, j, "request-journal-failed")
			return
		}
		var body, providerContent, providerReasoning strings.Builder
		var pending []ai.ToolCall
		var thinking []json.RawMessage
		var tokens map[string]int
		limited, rawProgress := false, false
		characters := 0
		updateProgress := func(force bool) {
			j.Progress.OutputTokens = completedTokens + (characters+2)/3
			j.Progress.TokensEstimated = true
			if value, ok := tokens["outputTokens"]; ok {
				j.Progress.OutputTokens = completedTokens + value
				j.Progress.TokensEstimated = completedTokensEstimated
			}
			if force || time.Since(lastProgress) >= 500*time.Millisecond {
				if s.phase(slug, &j, "generating") {
					lastProgress = time.Now()
				}
			}
		}
		err := s.gateway.Stream(ctx, req, func(ev ai.GatewayEvent) {
			switch ev.Type {
			case "output-limit":
				limited = true
			case "output-progress":
				rawProgress = true
				characters += ev.Usage["characters"]
			case "text-delta":
				body.WriteString(ev.Delta)
				if !rawProgress {
					characters += len([]rune(ev.Delta))
				}
			case "provider-content-delta":
				providerContent.WriteString(ev.Delta)
			case "provider-reasoning-delta":
				providerReasoning.WriteString(ev.Delta)
			case "thinking-block", "provider-thinking-block":
				if ev.ThinkingBlock != nil {
					thinking = append(thinking, ev.ThinkingBlock)
				}
			case "tool-call", "tool-call-ready":
				if ev.ToolCall != nil {
					pending = append(pending, *ev.ToolCall)
				}
			case "usage":
				if ev.Usage != nil {
					if tokens == nil {
						tokens = map[string]int{}
					}
					for key, value := range ev.Usage {
						tokens[key] = value
					}
				}
			}
			updateProgress(ev.Type == "usage")
			if body.Len()+providerContent.Len()+providerReasoning.Len() > 4<<20 || len(pending) > 12 {
				cancel()
			}
		})
		updateProgress(true)
		completedTokens = j.Progress.OutputTokens
		completedTokensEstimated = j.Progress.TokensEstimated
		s.mu.Lock()
		if saved, lookupErr := getJob(slug, j.ID); !s.deleted[slug] && lookupErr == nil && saved.ID == j.ID {
			usageErr := appendUsage(root, Usage{RequestID: requestID, JobID: j.ID, Attempt: j.Attempt, ProviderID: j.ProviderID, Model: j.Model, State: "request-finished", Known: tokens != nil, Tokens: tokens, CreatedAt: time.Now().UTC()})
			if err == nil {
				err = usageErr
			}
		}
		s.mu.Unlock()
		if err != nil || ctx.Err() != nil {
			code := "provider-failed"
			if ctx.Err() != nil {
				code = "request-interrupted"
			}
			s.fail(slug, j, code)
			return
		}
		if limited {
			s.fail(slug, j, "output-limit")
			return
		}
		if body.Len() > 4<<20 {
			s.fail(slug, j, "output-too-large")
			return
		}
		text = body.String()
		if len(pending) == 0 {
			break
		}
		if round == 7 || len(pending) > 8 {
			s.fail(slug, j, "tool-budget-exhausted")
			return
		}
		req.Messages = append(req.Messages, ai.GatewayMessage{Role: "assistant", Content: text, ToolCalls: pending, ThinkingBlocks: thinking, ProviderContent: providerContent.String(), ReasoningContent: providerReasoning.String()})
		for _, call := range pending {
			if call.ToolName == "search_web" {
				webCalls++
				query, ok := call.Arguments["query"].(string)
				if searcher == nil || webCalls > 2 || !ok || len(call.Arguments) != 1 || len([]rune(query)) > 256 || strings.TrimSpace(query) == "" {
					s.fail(slug, j, "invalid-provider-tool")
					return
				}
				results, searchErr := searcher.Search(ctx, query)
				response := map[string]any{"status": "unavailable", "message": "搜索未完成，不得声称最新信息已核验"}
				if searchErr == nil {
					if len(results) > 5 {
						results = results[:5]
					}
					response = map[string]any{"status": "ok", "results": results}
				}
				raw, _ := json.Marshal(response)
				if len(raw) > 16000 {
					s.fail(slug, j, "search-budget-exhausted")
					return
				}
				req.Messages = append(req.Messages, ai.GatewayMessage{Role: "tool", ToolCallID: call.ProviderCallID, Content: string(raw)})
				continue
			}
			sourceCalls++
			if sourceCalls > 12 || reader == nil {
				s.fail(slug, j, "tool-budget-exhausted")
				return
			}
			budget := min(4000, 12000-sourceTokens, max(0, available-1024))
			if budget < 128 {
				s.fail(slug, j, "source-budget-exhausted")
				return
			}
			result := executeSource(ctx, reader, f.Sources, call, budget)
			if result.Error != nil && (result.Error.Code == "invalid_arguments" || result.Error.Code == "invalid_tool") {
				s.fail(slug, j, "invalid-provider-tool")
				return
			}
			raw, _ := json.Marshal(result)
			sourceTokens += len([]rune(string(raw)))
			if data, ok := result.Data.(sources.SourceReadData); ok {
				j.Coverage.ReadRanges = append(j.Coverage.ReadRanges, data.Citation)
			}
			if data, ok := result.Data.(sources.SourceSearchData); ok {
				for _, hit := range data.Hits {
					j.Coverage.ReadRanges = append(j.Coverage.ReadRanges, hit.Citation)
				}
			}
			req.Messages = append(req.Messages, ai.GatewayMessage{Role: "tool", ToolCallID: call.ProviderCallID, Content: string(raw)})
		}
	}
	if !s.phase(slug, &j, "validating") {
		return
	}
	if strings.TrimSpace(text) == "" {
		s.fail(slug, j, "empty-output")
		return
	}
	if j.Kind == "practice" {
		if ValidatePractice(text, j.QuestionCount) != nil {
			s.fail(slug, j, "invalid-practice-output")
			return
		}
	}
	if j.Kind == "review" && !strings.Contains(text, "\n## ") && !strings.HasPrefix(text, "## ") {
		s.fail(slug, j, "invalid-review-output")
		return
	}
	var mapOutput MapProjection
	if j.Kind == "discipline-map" {
		var issue *MapValidationIssue
		mapOutput, issue = decodeMapOutput(text)
		if issue != nil {
			s.failMap(slug, j, text, issue)
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, e = getJob(slug, j.ID)
	if e != nil || current.Status != "running" || s.deleted[slug] || ctx.Err() != nil {
		return
	}
	if _, e = projects.ReadProjectState(slug); e != nil {
		return
	}
	j.Phase = "publishing"
	if e = s.saveLocked(slug, root, &j); e != nil {
		return
	}
	commit := func() error {
		if j.Kind == "discipline-map" {
			mapOutput.Coverage = j.Coverage
			version, e := publishMapLocked(slug, j.ID, f.BaseMapVersionID, mapOutput)
			if e != nil {
				return e
			}
			j.ResultRefs = []string{version}
		} else {
			st, e := assets.New(slug)
			if e != nil {
				return problem("asset-unavailable", 409)
			}
			ids := []string{}
			for _, d := range f.Sources {
				ids = append(ids, d.RevisionID)
			}
			result, e := st.CommitCandidate(assets.CandidateInput{Key: j.Kind, BaseVersionID: f.BaseAsset.Meta.CurrentVersionID, BaseContent: f.BaseAsset.Content, CandidateContent: text, TaskID: j.ID, RunID: j.ID, Author: "api-generation", JobID: j.ID, Attempt: j.Attempt, FromSeq: f.BaseAsset.Meta.ConversationCursor + 1, ThroughSeq: f.ThroughSeq, SourceRevisionIDs: ids, ChangeSummary: "API生成教学内容"})
			if e != nil || result.Asset == nil {
				return problem("edit-conflict", 409)
			}
			j.ResultRefs = []string{result.Asset.Meta.CurrentVersionID}
		}
		return nil
	}
	if len(f.Sources) > 0 {
		st, err := sources.New(slug)
		if err != nil {
			s.failLocked(slug, root, j, "source-unavailable")
			return
		}
		guards := []sources.RevisionGuard{}
		for _, d := range f.Sources {
			guards = append(guards, sources.RevisionGuard{SourceID: d.SourceID, RevisionID: d.RevisionID, ContentHash: d.ContentHash})
		}
		e = st.WithReadableRevisions(guards, commit)
	} else {
		e = commit()
	}
	if e != nil {
		s.failLocked(slug, root, j, ErrorCode(e))
		return
	}
	j.Status = "succeeded"
	j.Phase = "done"
	j.Progress.CompletedUnits, j.Progress.TotalUnits, j.Progress.UnitLabel, j.Progress.Indeterminate = 1, 1, "成果", false
	if j.Kind == "practice" {
		j.Progress.CompletedUnits, j.Progress.TotalUnits, j.Progress.UnitLabel = j.QuestionCount, j.QuestionCount, "题"
	}
	_ = s.saveLocked(slug, root, &j)
}

func webSearchTool() ai.Tool {
	return ai.Tool{Name: "search_web", Description: "受控检索公开信息，只有实际证据支持时才能声称最新进展已核验。", Schema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"query": map[string]any{"type": "string", "minLength": 1, "maxLength": 256}}, "required": []string{"query"}}}
}
func (s *Service) fail(slug string, j Job, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return
	}
	s.failLocked(slug, root, j, code)
}
func (s *Service) failLocked(slug, root string, j Job, code string) {
	current, e := getJob(slug, j.ID)
	if e != nil || current.Status != "running" || s.deleted[slug] {
		return
	}
	j.Status = "failed"
	if code == "request-interrupted" {
		j.Status = "interrupted"
	}
	j.Phase = "done"
	j.ErrorCode = code
	_ = s.saveLocked(slug, root, &j)
}
func executeSource(ctx context.Context, r *sources.Reader, docs []sources.SourceDocument, call ai.ToolCall, budget int) sources.SourceResult {
	decode := func(out any) error {
		raw, e := json.Marshal(call.Arguments)
		if e != nil {
			return e
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		return d.Decode(out)
	}
	invalid := sources.SourceResult{Status: "error", Error: &sources.SourceToolError{Code: "invalid_arguments", Message: "资料参数无效"}}
	switch call.ToolName {
	case "list_source_documents":
		var a sources.ListSourceArgs
		if decode(&a) != nil {
			return invalid
		}
		a.MaxTokens = budget / 2
		return r.List(ctx, docs, a)
	case "get_source_outline":
		var a sources.OutlineSourceArgs
		if decode(&a) != nil {
			return invalid
		}
		a.MaxTokens = budget / 2
		return r.Outline(ctx, docs, a)
	case "search_source":
		var a sources.SearchSourceArgs
		if decode(&a) != nil {
			return invalid
		}
		a.MaxTokens = max(1, (budget-700)/2)
		return r.Search(ctx, docs, a)
	case "read_source":
		var a sources.ReadSourceArgs
		if decode(&a) != nil || a.MaxTokens < 0 || a.MaxTokens > 4000 {
			return invalid
		}
		a.MaxTokens = min(max(1, a.MaxTokens), max(1, (budget-800)/2))
		return r.Read(ctx, docs, a)
	default:
		return sources.SourceResult{Status: "error", Error: &sources.SourceToolError{Code: "invalid_tool", Message: "未开放此工具"}}
	}
}
func cleanOutput(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") && strings.HasSuffix(s, "```") {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "```json"), "```"))
	} else if newline := strings.Index(s, "\n"); newline >= 0 && strings.HasSuffix(s, "\n```") {
		header := strings.TrimSpace(s[:newline])
		if header == "```" || strings.EqualFold(header, "```json") {
			s = strings.TrimSpace(s[newline+1 : len(s)-3])
		}
	}
	return s
}

// UsageRecords collapses request-started/finished into one record per actual
// request, retaining unknown usage after a crash and actual zero counts.
func UsageRecords(slug string) ([]Usage, error) {
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return nil, e
	}
	data, e := os.ReadFile(filepath.Join(root, "generation", "usage.jsonl"))
	if errors.Is(e, os.ErrNotExist) {
		return []Usage{}, nil
	}
	if e != nil {
		return nil, e
	}
	order := []string{}
	byID := map[string]Usage{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var u Usage
		if json.Unmarshal(line, &u) != nil {
			continue
		}
		if _, ok := byID[u.RequestID]; !ok {
			order = append(order, u.RequestID)
		}
		byID[u.RequestID] = u
	}
	out := []Usage{}
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, nil
}
