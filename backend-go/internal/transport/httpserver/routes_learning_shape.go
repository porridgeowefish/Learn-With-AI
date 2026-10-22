package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	progressstore "github.com/xmz14/lll/backend-go/internal/modules/activity"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"

	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
)

var completeLearningShapeAI = ai.Complete

type projectTypeAdviceMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type projectTypeAdvice struct {
	Reply          string                 `json:"reply"`
	Recommendation *workspace.ProjectType `json:"recommendation,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
	Tradeoff       string                 `json:"tradeoff,omitempty"`
	Confidence     string                 `json:"confidence,omitempty"`
}

type disciplineLearningTask struct {
	ID          string `json:"id"`
	TopicID     string `json:"topicId,omitempty"`
	TopicTitle  string `json:"topicTitle"`
	Status      string `json:"status"`
	AddedAt     string `json:"addedAt"`
	StartedAt   string `json:"startedAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

type disciplineLearningPlan struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Items         []disciplineLearningTask `json:"items"`
	UpdatedAt     string                   `json:"updatedAt"`
}

func configuredAskAIProvider() (ai.Provider, error) {
	cfg, err := ai.LoadConfig()
	if err != nil {
		return ai.Provider{}, err
	}
	if cfg == nil || !cfg.Enabled() {
		return ai.Provider{}, errors.New("ai_not_configured")
	}
	p := cfg.Find("")
	if p == nil {
		return ai.Provider{}, errors.New("ai_not_configured")
	}
	return ai.Provider{
		ID: p.ID, Kind: p.Kind, Name: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey,
		Model: p.Model, Reasoning: p.Reasoning, Thinking: p.Thinking,
	}, nil
}

func (s *Server) handleProjectTypeAdvice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title    string                     `json:"title"`
		Why      string                     `json:"why"`
		Current  string                     `json:"current"`
		Target   string                     `json:"target"`
		Standard string                     `json:"standard"`
		Messages []projectTypeAdviceMessage `json:"messages"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(in.Messages) == 0 || len(in.Messages) > 20 {
		httpx.Error(w, http.StatusBadRequest, "messages must contain 1 to 20 turns")
		return
	}
	for i := range in.Messages {
		in.Messages[i].Role = strings.TrimSpace(in.Messages[i].Role)
		in.Messages[i].Content = strings.TrimSpace(in.Messages[i].Content)
		if (in.Messages[i].Role != "user" && in.Messages[i].Role != "assistant") || in.Messages[i].Content == "" {
			httpx.Error(w, http.StatusBadRequest, "invalid conversation message")
			return
		}
	}
	if in.Messages[len(in.Messages)-1].Role != "user" {
		httpx.Error(w, http.StatusBadRequest, "last message must be from user")
		return
	}
	provider, err := configuredAskAIProvider()
	if err != nil {
		httpx.Error(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	system := `你是“学科地图 / 系统学习”选择顾问，进行简短中文对话，但不保存对话、不创建项目，也不替用户做决定。

判断标准：
- 用户想先看一门广泛学科的边界、主要研究领域、方法和路线，建议 discipline-map。
- 用户想掌握一个具体概念、方法、技能，或解决一类明确问题，建议 system-learning。
- 信息不足时先问一个最关键的澄清问题，不要强行推荐。
- draft 只包含用户在打开顾问前真实填写过的信息；缺失字段就是未知，禁止猜测用户当前水平、目标水平或完成标准。
- 已足够判断时，清楚说明建议、理由和选择另一种形态会损失什么。

JSON 字符串内部需要引用概念时使用全角引号「」，不要使用未转义的半角双引号。
只输出 JSON：
{"reply":"给用户的自然语言回复，不超过160字","recommendation":"discipline-map|system-learning|undetermined","reason":"不超过80字，可为空","tradeoff":"不超过80字，可为空","confidence":"low|medium|high"}`
	draft := map[string]string{}
	for key, value := range map[string]string{
		"title": in.Title, "why": in.Why, "current": in.Current,
		"target": in.Target, "standard": in.Standard,
	} {
		if value = strings.TrimSpace(value); value != "" {
			draft[key] = value
		}
	}
	payload := map[string]any{
		"draft":        draft,
		"conversation": in.Messages,
	}
	input, _ := json.Marshal(payload)
	out, err := completeLearningShapeAI(ctx, provider, system, []ai.AIMessage{{Role: "user", Content: string(input)}})
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "project type advice failed: "+err.Error())
		return
	}
	var raw struct {
		Reply          string `json:"reply"`
		Recommendation string `json:"recommendation"`
		Reason         string `json:"reason"`
		Tradeoff       string `json:"tradeoff"`
		Confidence     string `json:"confidence"`
	}
	if err := decodeAIJSON(out, &raw); err != nil {
		advice := projectTypeAdvice{
			Reply:      looseJSONTextField(out, "reply", "recommendation", "reason", "tradeoff", "confidence"),
			Reason:     looseJSONTextField(out, "reason", "tradeoff", "confidence"),
			Tradeoff:   looseJSONTextField(out, "tradeoff", "confidence"),
			Confidence: looseJSONTextField(out, "confidence"),
		}
		if advice.Reply == "" {
			advice.Reply = cleanAIText(out)
		}
		if advice.Reply == "" {
			httpx.Error(w, http.StatusBadGateway, "project type advisor returned no content")
			return
		}
		if advice.Confidence != "low" && advice.Confidence != "medium" && advice.Confidence != "high" {
			advice.Confidence = "low"
		}
		recommendation := workspace.ProjectType(looseJSONTextField(out, "recommendation", "reason", "tradeoff", "confidence"))
		if workspace.ValidateProjectType(recommendation) && advice.Reason != "" && advice.Tradeoff != "" {
			advice.Recommendation = &recommendation
		}
		httpx.WriteJSON(w, http.StatusOK, advice)
		return
	}
	if strings.TrimSpace(raw.Reply) == "" {
		raw.Reply = strings.TrimSpace(raw.Reason)
	}
	if strings.TrimSpace(raw.Reply) == "" {
		httpx.Error(w, http.StatusBadGateway, "project type advisor returned no reply")
		return
	}
	if raw.Confidence != "low" && raw.Confidence != "medium" && raw.Confidence != "high" {
		raw.Confidence = "low"
	}
	advice := projectTypeAdvice{
		Reply: strings.TrimSpace(raw.Reply), Reason: strings.TrimSpace(raw.Reason),
		Tradeoff: strings.TrimSpace(raw.Tradeoff), Confidence: raw.Confidence,
	}
	if raw.Recommendation != "undetermined" {
		projectType := workspace.ProjectType(raw.Recommendation)
		if workspace.ValidateProjectType(projectType) && advice.Reason != "" && advice.Tradeoff != "" {
			advice.Recommendation = &projectType
		}
	}
	httpx.WriteJSON(w, http.StatusOK, advice)
}

func cleanAIText(text string) string {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 {
			lines = lines[1:]
			if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
				lines = lines[:len(lines)-1]
			}
			text = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return text
}

// looseJSONTextField recovers a known string field when a model returns
// JSON-shaped text containing unescaped quotes inside a value. It uses the
// following known field boundary rather than treating the first quote as the
// end of the value.
func looseJSONTextField(text, key string, nextKeys ...string) string {
	text = cleanAIText(text)
	marker := `"` + key + `"`
	start := strings.Index(text, marker)
	if start < 0 {
		return ""
	}
	rest := text[start+len(marker):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:]
	end := -1
	for _, nextKey := range nextKeys {
		boundary := `","` + nextKey + `"`
		if index := strings.Index(rest, boundary); index >= 0 && (end < 0 || index < end) {
			end = index
		}
	}
	if end < 0 {
		if index := strings.LastIndex(rest, `"}`); index >= 0 {
			end = index
		} else if index := strings.LastIndex(rest, `"`); index >= 0 {
			end = index
		}
	}
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func (s *Server) handleGetDisciplineLearningPlan(w http.ResponseWriter, r *http.Request) {
	_, root, ok := requireDisciplineMap(w, r.PathValue("id"))
	if !ok {
		return
	}
	plan, err := readDisciplineLearningPlan(filepath.Join(root, "learning-plan.json"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read_learning_plan")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, plan)
}

func (s *Server) handlePutDisciplineLearningPlan(w http.ResponseWriter, r *http.Request) {
	_, root, ok := requireDisciplineMap(w, r.PathValue("id"))
	if !ok {
		return
	}
	var plan disciplineLearningPlan
	if err := httpx.ReadJSON(r, &plan); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := validateDisciplineLearningPlan(&plan); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range plan.Items {
		item := &plan.Items[i]
		if item.Status == "in-progress" && item.StartedAt == "" {
			item.StartedAt = now
		}
		if item.Status == "completed" {
			if item.StartedAt == "" {
				item.StartedAt = now
			}
			if item.CompletedAt == "" {
				item.CompletedAt = now
			}
		}
	}
	plan.SchemaVersion = 1
	plan.UpdatedAt = now
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "encode_learning_plan")
		return
	}
	if err := filesystem.AtomicWriteFile(filepath.Join(root, "learning-plan.json"), raw, 0o644); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "write_learning_plan")
		return
	}
	for _, item := range plan.Items {
		if item.Status != "completed" {
			continue
		}
		if _, _, err := s.recordLearningActivity(r.PathValue("id"), progressstore.ProgressEvent{
			ID:            "learning-task-complete:" + item.ID + ":" + item.CompletedAt,
			SourceType:    "learning-task-complete",
			SourceID:      item.ID,
			Outcome:       "completed",
			ActivityDelta: 1,
			Title:         "完成学习任务",
			Detail:        item.TopicTitle,
		}); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "record_learning_task_activity")
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, plan)
}

func readDisciplineLearningPlan(path string) (disciplineLearningPlan, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return disciplineLearningPlan{SchemaVersion: 1, Items: []disciplineLearningTask{}}, nil
	}
	if err != nil {
		return disciplineLearningPlan{}, err
	}
	var plan disciplineLearningPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return disciplineLearningPlan{}, err
	}
	if plan.Items == nil {
		plan.Items = []disciplineLearningTask{}
	}
	return plan, nil
}

func validateDisciplineLearningPlan(plan *disciplineLearningPlan) error {
	if len(plan.Items) > 200 {
		return errors.New("learning_plan_too_large")
	}
	seenIDs := map[string]bool{}
	seenTopics := map[string]bool{}
	for i := range plan.Items {
		item := &plan.Items[i]
		item.ID = strings.TrimSpace(item.ID)
		item.TopicID = strings.TrimSpace(item.TopicID)
		item.TopicTitle = strings.TrimSpace(item.TopicTitle)
		if item.ID == "" || len(item.ID) > 120 || len(item.TopicID) > 120 ||
			item.TopicTitle == "" || len(item.TopicTitle) > 180 {
			return errors.New("invalid_learning_task")
		}
		if !validLearningTaskTime(item.AddedAt, false) ||
			!validLearningTaskTime(item.StartedAt, true) ||
			!validLearningTaskTime(item.CompletedAt, true) {
			return errors.New("invalid_learning_task_time")
		}
		if seenIDs[item.ID] || seenTopics[item.TopicTitle] {
			return errors.New("duplicate_learning_task")
		}
		seenIDs[item.ID] = true
		seenTopics[item.TopicTitle] = true
		switch item.Status {
		case "planned":
			if item.StartedAt != "" || item.CompletedAt != "" {
				return errors.New("invalid_learning_task_status_time")
			}
		case "in-progress":
			if item.CompletedAt != "" {
				return errors.New("invalid_learning_task_status_time")
			}
		case "completed":
		default:
			return errors.New("invalid_learning_task_status")
		}
	}
	return nil
}

func validLearningTaskTime(value string, optional bool) bool {
	if value == "" {
		return optional
	}
	if len(value) > 40 {
		return false
	}
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func requireDisciplineMap(w http.ResponseWriter, slug string) (*workspace.ProjectState, string, bool) {
	if !workspace.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug")
		return nil, "", false
	}
	state, err := workspace.ReadProjectState(slug)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "project_not_found")
		return nil, "", false
	}
	if state.ProjectType != workspace.ProjectTypeDisciplineMap {
		httpx.Error(w, http.StatusBadRequest, "invalid_project_type")
		return nil, "", false
	}
	root, err := workspace.ProjectRootForSlug(slug)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return nil, "", false
	}
	return state, root, true
}

func decodeAIJSON(text string, target any) error {
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 {
			lines = lines[1:]
			if strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
				lines = lines[:len(lines)-1]
			}
			text = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return errors.New("JSON object not found")
	}
	return json.Unmarshal([]byte(text[start:end+1]), target)
}
