// Package generation owns independent, durable API learning jobs. It never
// invokes learning CLI execution or the teacher conversation turn lifecycle.
package generation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	sources "github.com/xmz14/lll/backend-go/internal/modules/sources"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	"github.com/xmz14/lll/backend-go/internal/modules/assets"
	"github.com/xmz14/lll/backend-go/internal/modules/preferences"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"

	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
)

type Input struct {
	OperationID   string   `json:"operationId"`
	Kind          string   `json:"kind"`
	ProviderID    string   `json:"providerId,omitempty"`
	Model         string   `json:"model,omitempty"`
	SourceIDs     []string `json:"sourceIds"`
	QuestionCount int      `json:"questionCount,omitempty"`
	MapMode       string   `json:"mapMode,omitempty"`
}
type Progress struct {
	OutputTokens    int    `json:"outputTokens,omitempty"`
	TokensEstimated bool   `json:"tokensEstimated"`
	CompletedUnits  int    `json:"completedUnits,omitempty"`
	TotalUnits      int    `json:"totalUnits,omitempty"`
	UnitLabel       string `json:"unitLabel,omitempty"`
	Indeterminate   bool   `json:"indeterminate"`
}
type Coverage struct {
	Sources             []sources.SourceDocument `json:"sources"`
	ReadRanges          []sources.SourceCitation `json:"readRanges"`
	CompleteWithinScope bool                     `json:"completeWithinScope"`
	Gap                 string                   `json:"gap,omitempty"`
}
type Job struct {
	Checkpoint         *Checkpoint         `json:"checkpoint,omitempty"`
	SchemaVersion      int                 `json:"schemaVersion"`
	ID                 string              `json:"id"`
	RootJobID          string              `json:"rootJobId"`
	RetryOf            string              `json:"retryOf,omitempty"`
	Attempt            int                 `json:"attempt"`
	OperationID        string              `json:"operationId"`
	Kind               string              `json:"kind"`
	Status             string              `json:"status"`
	Phase              string              `json:"phase"`
	ProviderID         string              `json:"providerId"`
	Model              string              `json:"model"`
	QuestionCount      int                 `json:"questionCount,omitempty"`
	MapMode            string              `json:"mapMode,omitempty"`
	Progress           Progress            `json:"progress"`
	Coverage           Coverage            `json:"coverage"`
	ResultRefs         []string            `json:"resultRefs"`
	ErrorCode          string              `json:"errorCode,omitempty"`
	Validation         *MapValidationIssue `json:"validation,omitempty"`
	CandidateAvailable bool                `json:"candidateAvailable,omitempty"`
	CreatedAt          time.Time           `json:"createdAt"`
	UpdatedAt          time.Time           `json:"updatedAt"`
	Intent             Input               `json:"intent"`
	RequestIDs         []string            `json:"requestIds,omitempty"`
}
type Checkpoint struct {
	AfterSeq            uint64 `json:"afterSeq"`
	ThroughSeq          uint64 `json:"throughSeq"`
	HasMoreConversation bool   `json:"hasMoreConversation"`
}
type frozenInput struct {
	MaxOutputTokens   int                      `json:"maxOutputTokens,omitempty"`
	MaxThinkingTokens int                      `json:"maxThinkingTokens,omitempty"`
	WebSearchEnabled  bool                     `json:"webSearchEnabled"`
	System            string                   `json:"system"`
	PromptHash        string                   `json:"promptHash"`
	PromptVersion     string                   `json:"promptVersion"`
	Messages          []ai.GatewayMessage      `json:"messages"`
	Sources           []sources.SourceDocument `json:"sources"`
	BaseAsset         assets.Asset             `json:"baseAsset"`
	BaseMapVersionID  string                   `json:"baseMapVersionId"`
	ThroughSeq        uint64                   `json:"throughSeq"`
	PreferencesHash   string                   `json:"preferencesHash"`
	InitialReadRanges []sources.SourceCitation `json:"initialReadRanges,omitempty"`
}
type APIError struct {
	Code   string
	Status int
	JobID  string
}

func (e *APIError) Error() string           { return e.Code }
func problem(code string, status int) error { return &APIError{Code: code, Status: status} }
func ErrorCode(err error) string {
	var e *APIError
	if errors.As(err, &e) {
		return e.Code
	}
	return "generation-failed"
}
func ErrorStatus(err error) int {
	var e *APIError
	if errors.As(err, &e) {
		return e.Status
	}
	if errors.Is(err, os.ErrNotExist) {
		return 404
	}
	return 500
}

type Service struct {
	mu        sync.Mutex
	gateway   ai.Gateway
	active    map[string]context.CancelFunc
	deleted   map[string]bool
	notify    chan struct{}
	stop      context.CancelFunc
	workers   sync.WaitGroup
	OnUpdated func(string, Job)
}

func New(gateway ai.Gateway) *Service {
	if gateway == nil {
		gateway = ai.Configured{}
	}
	return &Service{gateway: gateway, active: map[string]context.CancelFunc{}, deleted: map[string]bool{}, notify: make(chan struct{}, 1)}
}

var operationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func normalize(in Input) (Input, error) {
	in.OperationID = strings.TrimSpace(in.OperationID)
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.Model = strings.TrimSpace(in.Model)
	if !operationPattern.MatchString(in.OperationID) || len(in.Model) > 256 || len(in.ProviderID) > 128 {
		return in, problem("invalid-generation-input", 400)
	}
	if in.Kind != "review" && in.Kind != "practice" && in.Kind != "discipline-map" {
		return in, problem("invalid-generation-kind", 400)
	}
	if in.Kind == "practice" {
		if in.QuestionCount == 0 {
			in.QuestionCount = 10
		}
		if in.QuestionCount < 1 || in.QuestionCount > 50 {
			return in, problem("invalid-question-count", 400)
		}
	} else if in.QuestionCount != 0 {
		return in, problem("invalid-question-count", 400)
	}
	if in.Kind == "discipline-map" {
		if in.MapMode != "general" && in.MapMode != "sources" {
			return in, problem("invalid-map-mode", 400)
		}
	} else if in.MapMode != "" {
		return in, problem("invalid-map-mode", 400)
	}
	if len(in.SourceIDs) > 32 {
		return in, problem("source-selection-required", 400)
	}
	if in.SourceIDs != nil {
		seen := map[string]bool{}
		ids := []string{}
		for _, id := range in.SourceIDs {
			if id == "" {
				return in, problem("invalid-source", 400)
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		in.SourceIDs = ids
	}
	if in.Kind == "discipline-map" && in.MapMode == "general" && len(in.SourceIDs) > 0 {
		return in, problem("general-map-does-not-use-sources", 400)
	}
	if in.MapMode == "sources" && len(in.SourceIDs) == 0 {
		return in, problem("source-selection-required", 400)
	}
	return in, nil
}
func sameIntent(a, b Input) bool {
	a.OperationID = ""
	b.OperationID = ""
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func jobDir(root string) string { return filepath.Join(root, "generation", "jobs") }
func inputPath(root, id string) string {
	return filepath.Join(root, "generation", "inputs", id, "input.json")
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	return filesystem.AtomicWriteFile(path, b, 0600)
}
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func loadJobs(root string) ([]Job, error) {
	entries, e := os.ReadDir(jobDir(root))
	if errors.Is(e, os.ErrNotExist) {
		return []Job{}, nil
	}
	if e != nil {
		return nil, e
	}
	out := []Job{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var j Job
		if e = readJSON(filepath.Join(jobDir(root), entry.Name()), &j); e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}
func resolveProvider(in Input) (ai.ProviderConfig, error) {
	cfg, e := ai.LoadConfig()
	if e != nil || cfg == nil {
		return ai.ProviderConfig{}, problem("provider-unavailable", 409)
	}
	name := in.Kind
	if name == "discipline-map" {
		name = "disciplineMap"
	}
	binding, configured := cfg.Bindings[name]
	if !configured {
		binding, configured = cfg.Bindings["teacher"]
		if !configured {
			binding = ai.Binding{ProviderID: cfg.Default}
		}
	}
	id := binding.ProviderID
	if in.ProviderID != "" {
		id = in.ProviderID
	}
	p := cfg.FindExact(id)
	if p == nil || p.APIKey == "" || p.BaseURL == "" || (p.Kind != "openai" && p.Kind != "anthropic") {
		return ai.ProviderConfig{}, problem("provider-unavailable", 409)
	}
	chosen := *p
	if in.Model != "" {
		chosen.Model = in.Model
	} else if binding.ProviderID == id && binding.Model != "" {
		chosen.Model = binding.Model
	}
	if chosen.Model == "" || strings.ContainsAny(chosen.Model, "\r\n\x00") {
		return chosen, problem("model-unavailable", 409)
	}
	return chosen, nil
}
func prompt(kind string) (string, string, error) {
	names := []string{"common", kind}
	if kind == "review" {
		for _, p := range []string{"research_question_frame", "prerequisite_scaffold", "misconception", "boundary_map", "analogy", "mece_decompose", "first_principles", "concept_graph"} {
			names = append(names, "primitives/"+p)
		}
	}
	if kind == "practice" {
		names = append(names, "primitives/transfer")
	}
	var manifest struct {
		Version   string `json:"version"`
		Resources []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"resources"`
	}
	if e := readJSON(filepath.Join(filesystem.PROJECT_ROOT, "learning-prompts", "manifest.json"), &manifest); e != nil {
		return "", "", problem("prompt-resource-unavailable", 503)
	}
	hashes := map[string]string{}
	for _, r := range manifest.Resources {
		hashes[r.Path] = r.SHA256
	}
	var b strings.Builder
	for _, n := range names {
		data, e := os.ReadFile(filepath.Join(filesystem.PROJECT_ROOT, "learning-prompts", n+".md"))
		if e != nil || hashes["learning-prompts/"+n+".md"] != hash(string(data)) {
			return "", "", problem("prompt-resource-unavailable", 503)
		}
		b.Write(data)
		b.WriteString("\n\n")
	}
	return b.String(), manifest.Version, nil
}
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func (s *Service) Create(ctx context.Context, slug string, in Input) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createLocked(ctx, slug, in, nil)
}
func (s *Service) createLocked(ctx context.Context, slug string, in Input, parent *Job) (Job, error) {
	in, e := normalize(in)
	if e != nil {
		return Job{}, e
	}
	if !projects.ValidateSlug(slug) {
		return Job{}, problem("invalid-project", 400)
	}
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return Job{}, e
	}
	jobs, e := loadJobs(root)
	if e != nil {
		return Job{}, e
	}
	for _, j := range jobs {
		if j.OperationID == in.OperationID {
			if !sameIntent(j.Intent, in) {
				return Job{}, problem("operation-conflict", 409)
			}
			return j, nil
		}
	}
	if s.deleted[slug] {
		return Job{}, problem("project-deleting", 410)
	}
	state, e := projects.ReadProjectState(slug)
	if e != nil {
		return Job{}, e
	}
	if (in.Kind == "discipline-map") != (state.ProjectType == projects.ProjectTypeDisciplineMap) {
		return Job{}, problem("generation-project-type", 400)
	}
	for _, j := range jobs {
		if j.Kind == in.Kind && (j.Status == "queued" || j.Status == "running") {
			return Job{}, &APIError{Code: "generation-active", Status: 409, JobID: j.ID}
		}
	}
	all, e := projects.IndexAll()
	if e != nil {
		return Job{}, e
	}
	queued := 0
	for _, p := range all {
		r, e := projects.ProjectRootForSlug(p.Slug)
		if e != nil {
			return Job{}, e
		}
		list, e := loadJobs(r)
		if e != nil {
			return Job{}, e
		}
		for _, j := range list {
			if j.Status == "queued" {
				queued++
			}
		}
	}
	if queued >= 32 {
		return Job{}, problem("generation-queue-full", 429)
	}
	p, e := resolveProvider(in)
	if e != nil {
		return Job{}, e
	}
	system, version, e := prompt(in.Kind)
	if e != nil {
		return Job{}, e
	}
	f := frozenInput{System: system, PromptHash: hash(system), PromptVersion: version, Sources: []sources.SourceDocument{}}
	if !(in.Kind == "discipline-map" && in.MapMode == "general") {
		store, e := sources.New(slug)
		if e != nil {
			return Job{}, e
		}
		docs, e := sources.NewReader(store).Snapshot(ctx)
		if e != nil {
			return Job{}, e
		}
		byID := map[string]sources.SourceDocument{}
		for _, d := range docs {
			byID[d.SourceID] = d
			if in.SourceIDs == nil && d.Readable {
				f.Sources = append(f.Sources, d)
			}
		}
		if in.SourceIDs != nil {
			for _, id := range in.SourceIDs {
				d, ok := byID[id]
				if !ok {
					return Job{}, problem("source-not-found", 404)
				}
				if !d.Readable {
					return Job{}, problem("source-not-readable", 409)
				}
				f.Sources = append(f.Sources, d)
			}
		}
		if len(f.Sources) > 32 {
			return Job{}, problem("source-selection-required", 400)
		}
	}
	payload := map[string]any{"date": time.Now().Format("2006-01-02"), "project": state, "questionCount": in.QuestionCount, "mapMode": in.MapMode, "sourceDocuments": f.Sources}
	if pref, e := preferences.Read(); e != nil {
		return Job{}, e
	} else {
		payload["learnerPreferences"] = pref.Content
		f.PreferencesHash = hash(pref.Content)
	}
	for _, name := range []string{"project.md", "learning-scope.json", "intro/output.md"} {
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if e == nil {
			if len(b) > 256<<10 {
				return Job{}, problem("input-budget-exhausted", 409)
			}
			payload[name] = string(b)
		} else if !errors.Is(e, os.ErrNotExist) {
			return Job{}, e
		}
	}
	if in.Kind != "discipline-map" {
		st, e := assets.New(slug)
		if e != nil {
			return Job{}, e
		}
		f.BaseAsset, e = st.Get(in.Kind)
		if e != nil {
			return Job{}, e
		}
		payload["currentAsset"] = f.BaseAsset.Content
		// Only published work advances this checkpoint. Failed/cancelled jobs
		// must never make the next summary skip unprocessed conversation.
		previousRanges := []sources.SourceCitation{}
		for _, previous := range jobs {
			if previous.Kind != in.Kind || previous.Status != "succeeded" {
				continue
			}
			for _, citation := range previous.Coverage.ReadRanges {
				for _, doc := range f.Sources {
					if citation.SourceID == doc.SourceID && citation.RevisionID == doc.RevisionID && citation.ContentHash == doc.ContentHash {
						previousRanges = append(previousRanges, citation)
					}
				}
			}
			break
		}
		f.InitialReadRanges = previousRanges
		payload["checkpoint"] = map[string]any{"afterSeq": f.BaseAsset.Meta.ConversationCursor, "readRanges": previousRanges}
		review, err := st.Get("review")
		if err != nil {
			return Job{}, err
		}
		if in.Kind != "review" {
			payload["review"] = review.Content
		}
		annotations, err := assets.NewAnnotations(slug)
		if err != nil {
			return Job{}, err
		}
		allAnnotations, err := annotations.List(false)
		if err != nil {
			return Job{}, err
		}
		confusions := []map[string]string{}
		for _, a := range allAnnotations {
			if a.Status == assets.StateOpen || a.Status == assets.StateAsked {
				confusions = append(confusions, map[string]string{"quote": a.QuoteSnapshot, "question": a.Note})
			}
		}
		payload["unresolvedConfusions"] = confusions
		notes, err := teacher.NewNoteStore(slug)
		if err != nil {
			return Job{}, err
		}
		noteSnapshot, err := notes.List()
		if err != nil {
			return Job{}, err
		}
		payload["notes"] = noteSnapshot.Entries
		c, e := teacher.NewConversation(slug)
		if e != nil {
			return Job{}, e
		}
		m, e := c.Meta()
		if e != nil {
			return Job{}, e
		}
		f.ThroughSeq = m.LatestSeq
		proj, e := c.SequencedMessagesThrough(m.LatestSeq)
		if e != nil {
			return Job{}, e
		}
		messages := []map[string]any{}
		for _, sequenced := range proj {
			if sequenced.Seq <= f.BaseAsset.Meta.ConversationCursor || sequenced.Seq > m.LatestSeq {
				continue
			}
			message := sequenced.Message
			if message.Status != "completed" {
				continue
			}
			parts := []string{}
			for _, block := range message.Blocks {
				if block.Type == "markdown" {
					parts = append(parts, block.Source)
				}
			}
			if len(parts) > 0 {
				messages = append(messages, map[string]any{"seq": sequenced.Seq, "role": message.Role, "content": strings.Join(parts, "\n\n")})
			}
		}
		payload["conversation"] = messages
		payload["conversationRange"] = map[string]uint64{"fromSeq": f.BaseAsset.Meta.ConversationCursor + 1, "throughSeq": f.ThroughSeq}
	} else {
		m, e := ReadMap(slug, "")
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return Job{}, e
		}
		f.BaseMapVersionID = m.MapVersionID
		if in.MapMode == "sources" {
			outline, samples, e := sourceMapContext(ctx, slug, f.Sources)
			if e != nil {
				return Job{}, e
			}
			payload["sourceOutlines"] = outline
			for _, sample := range samples {
				f.InitialReadRanges = append(f.InitialReadRanges, sample.Citation)
			}
			payload["sourceVerificationSamples"] = samples
		}
	}
	raw, e := json.Marshal(payload)
	if e != nil {
		return Job{}, e
	}
	f.Messages = []ai.GatewayMessage{{Role: "user", Content: string(raw)}}
	f.MaxOutputTokens = ai.GenerationOutputTokens(p)
	f.MaxThinkingTokens = ai.GenerationThinkingTokens(p)
	req := ai.GatewayRequest{System: system, Messages: f.Messages, ProviderID: p.ID, Model: p.Model, MaxOutputTokens: f.MaxOutputTokens, MaxThinkingTokens: f.MaxThinkingTokens}
	if len(f.Sources) > 0 {
		req.Tools = sources.Tools()
	}
	f.WebSearchEnabled = teacher.ConfiguredWebSearch() != nil
	if f.WebSearchEnabled {
		req.Tools = append(req.Tools, webSearchTool())
	}
	if _, e = ai.CheckGenerationRequest(req, p); e != nil && in.Kind != "discipline-map" {
		// Seal the largest complete prefix that fits. The rest remains after
		// the committed cursor for the next explicit summary; no message is sliced.
		messages, _ := payload["conversation"].([]map[string]any)
		for e != nil && len(messages) > 1 {
			messages = messages[:len(messages)-1]
			f.ThroughSeq = messages[len(messages)-1]["seq"].(uint64)
			payload["conversation"] = messages
			payload["conversationRange"] = map[string]uint64{"fromSeq": f.BaseAsset.Meta.ConversationCursor + 1, "throughSeq": f.ThroughSeq}
			payload["hasMoreConversation"] = true
			raw, e = json.Marshal(payload)
			if e != nil {
				return Job{}, e
			}
			f.Messages[0].Content = string(raw)
			req.Messages = f.Messages
			_, e = ai.CheckGenerationRequest(req, p)
		}
	}
	if e != nil {
		return Job{}, problem("input-budget-exhausted", 409)
	}
	now := time.Now().UTC()
	id := idgen.New("gen")
	j := Job{SchemaVersion: 1, ID: id, RootJobID: id, Attempt: 1, OperationID: in.OperationID, Kind: in.Kind, Status: "queued", Phase: "waiting", ProviderID: p.ID, Model: p.Model, QuestionCount: in.QuestionCount, MapMode: in.MapMode, Progress: Progress{Indeterminate: true}, Coverage: Coverage{Sources: f.Sources, ReadRanges: append([]sources.SourceCitation{}, f.InitialReadRanges...)}, ResultRefs: []string{}, CreatedAt: now, UpdatedAt: now, Intent: in}
	if in.Kind == "discipline-map" && in.MapMode == "sources" {
		j.Coverage.Gap = "地图依据当前已提取文字与有限阅读整理；章节可由正文归纳，不表示已读完整份原件。未识别页和公式仍需核对。"
	}
	if in.Kind != "discipline-map" {
		more, _ := payload["hasMoreConversation"].(bool)
		j.Checkpoint = &Checkpoint{AfterSeq: f.BaseAsset.Meta.ConversationCursor, ThroughSeq: f.ThroughSeq, HasMoreConversation: more}
	}
	if parent != nil {
		j.RootJobID = parent.RootJobID
		j.RetryOf = parent.ID
		j.Attempt = parent.Attempt + 1
	}
	if e = writeJSON(inputPath(root, id), f); e != nil {
		return Job{}, e
	}
	if e = s.saveLocked(slug, root, &j); e != nil {
		return Job{}, e
	}
	s.wake()
	return j, nil
}
func (s *Service) saveLocked(slug, root string, j *Job) error {
	j.UpdatedAt = time.Now().UTC()
	if e := writeJSON(filepath.Join(jobDir(root), j.ID+".json"), j); e != nil {
		return e
	}
	if s.OnUpdated != nil {
		s.OnUpdated(slug, *j)
	}
	return nil
}
func (s *Service) wake() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}
func (s *Service) Get(slug, id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return getJob(slug, id)
}
func getJob(slug, id string) (Job, error) {
	if !projects.ValidateSlug(slug) || !safeID(id) {
		return Job{}, problem("invalid-job-id", 400)
	}
	r, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return Job{}, e
	}
	var j Job
	e = readJSON(filepath.Join(jobDir(r), id+".json"), &j)
	return j, e
}
func safeID(id string) bool {
	return len(id) > 0 && len(id) < 160 && regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(id)
}
func (s *Service) List(slug string, limit int, cursor string) ([]Job, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return nil, "", e
	}
	list, e := loadJobs(root)
	if e != nil {
		return nil, "", e
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		return nil, "", problem("invalid-limit", 400)
	}
	start := 0
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return nil, "", problem("invalid-cursor", 400)
		}
		found := false
		for i, j := range list {
			if j.ID == string(b) {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, "", problem("invalid-cursor", 400)
		}
	}
	end := min(start+limit, len(list))
	next := ""
	if end < len(list) {
		next = base64.RawURLEncoding.EncodeToString([]byte(list[end-1].ID))
	}
	return list[start:end], next, nil
}
func (s *Service) Cancel(slug, id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := getJob(slug, id)
	if e != nil {
		return Job{}, e
	}
	if j.Status != "queued" && j.Status != "running" {
		return j, nil
	}
	root, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return Job{}, e
	}
	j.Status = "cancelled"
	j.Phase = "done"
	if e = s.saveLocked(slug, root, &j); e != nil {
		return Job{}, e
	}
	if cancel := s.active[id]; cancel != nil {
		cancel()
	}
	return j, nil
}
func (s *Service) Retry(ctx context.Context, slug, id, operation string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := getJob(slug, id)
	if e != nil {
		return Job{}, e
	}
	in := j.Intent
	in.OperationID = operation
	root, _ := projects.ProjectRootForSlug(slug)
	list, e := loadJobs(root)
	if e != nil {
		return Job{}, e
	}
	for _, v := range list {
		if v.OperationID == operation {
			if v.RetryOf != id || !sameIntent(v.Intent, in) {
				return Job{}, problem("operation-conflict", 409)
			}
			return v, nil
		}
	}
	if j.Status != "failed" && j.Status != "interrupted" && j.Status != "cancelled" {
		return Job{}, problem("generation-not-retryable", 409)
	}
	return s.createLocked(ctx, slug, in, &j)
}

// DeleteProject sorts deletion against cancellation and publication using the
// same lifecycle lock. Provider callbacks can never recreate the removed tree.
func (s *Service) DeleteProject(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted[slug] = true
	r, e := projects.ProjectRootForSlug(slug)
	if e != nil {
		return e
	}
	list, e := loadJobs(r)
	if e != nil {
		return e
	}
	for _, j := range list {
		if cancel := s.active[j.ID]; cancel != nil {
			cancel()
		}
	}
	if e = sources.CancelProjectStructureJobs(slug); e != nil {
		return e
	}
	st, e := sources.New(slug)
	if e != nil {
		return e
	}
	return st.WithProjectDeletion(func() error { return projects.DeleteProject(slug) })
}

// ReviveProject is called after a fresh skeleton is created. Old completions
// additionally require their original job record to still exist.
func (s *Service) ReviveProject(slug string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.deleted, slug)
}

// SourceMutation prevents source deletion from crossing final generation
// validation/publication. Both HTTP source deletion operations use this seam.
func (s *Service) SourceMutation(mutate func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return mutate()
}
