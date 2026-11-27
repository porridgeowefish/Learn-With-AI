package generation

import (
	"context"
	"encoding/json"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/sources"
)

// Freeze document identity at submission and collect the text once when the
// job starts. Old queued inputs also use their frozen source revisions.
func prepareMapSources(ctx context.Context, slug string, req *ai.GatewayRequest, provider ai.ProviderConfig, docs []sources.SourceDocument) (Coverage, error) {
	if len(docs) == 0 {
		return Coverage{}, problem("source-selection-required", 400)
	}
	var payload map[string]json.RawMessage
	if len(req.Messages) != 1 || json.Unmarshal([]byte(req.Messages[0].Content), &payload) != nil || payload == nil {
		return Coverage{}, problem("input-unavailable", 409)
	}
	for _, key := range []string{"sourceDocuments", "sourceOutlines", "sourceVerificationSamples", "questionCount", "date"} {
		delete(payload, key)
	}
	var project struct {
		Title string `json:"title"`
	}
	if json.Unmarshal(payload["project"], &project) == nil && project.Title != "" {
		payload["project"], _ = json.Marshal(project)
	}
	base, err := json.Marshal(payload)
	if err != nil {
		return Coverage{}, err
	}
	req.Messages[0].Content = string(base)
	req.TurnInstructions = "根据 sourceTexts 里的课件原文生成学科路线。按实际主题组织学习顺序。partial 表示只提供了部分原文，［…］标出节选间隔。直接提交完整地图 JSON，并说明资料覆盖范围。"
	room, err := ai.CheckGenerationRequest(*req, provider)
	if err != nil {
		return Coverage{}, problem("input-budget-exhausted", 409)
	}
	const maxMapTextTokens = 16000
	budget := min(maxMapTextTokens, room/2)
	if budget < 3*len(docs) {
		return Coverage{}, problem("input-budget-exhausted", 409)
	}
	coverage, texts, err := sourceMapContext(ctx, slug, docs, budget)
	if err != nil {
		return Coverage{}, err
	}
	payload["sourceTexts"], err = json.Marshal(texts)
	if err != nil {
		return Coverage{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Coverage{}, err
	}
	req.Messages[0].Content = string(raw)
	return coverage, nil
}

func sourceMapContext(ctx context.Context, slug string, docs []sources.SourceDocument, budget int) (Coverage, []sources.PromptSourceText, error) {
	store, err := sources.New(slug)
	if err != nil {
		return Coverage{}, nil, err
	}
	reader := sources.NewReader(store)
	coverage := Coverage{Sources: docs, CompleteWithinScope: true}
	texts := make([]sources.PromptSourceText, 0, len(docs))
	perSource := min(4000, budget/len(docs))
	for _, doc := range docs {
		text, citations, err := reader.TextForPrompt(ctx, doc, perSource)
		if err != nil {
			return Coverage{}, nil, problem("source-verification-unavailable", 409)
		}
		coverage.ReadRanges = append(coverage.ReadRanges, citations...)
		texts = append(texts, text)
		coverage.CompleteWithinScope = coverage.CompleteWithinScope && !text.Partial
	}
	if !coverage.CompleteWithinScope {
		coverage.Gap = "部分资料采用原文节选，或仍有未识别内容。"
	}
	return coverage, texts, nil
}
