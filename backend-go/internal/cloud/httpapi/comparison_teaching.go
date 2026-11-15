package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/inference"
	"github.com/xmz14/lll/backend-go/internal/cloud/promptcatalog"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
	"github.com/xmz14/lll/backend-go/internal/idgen"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

func comparisonRequest(input llmcloud.ComparisonInput, pack promptcatalog.ComparisonPack, legacyRules string) (ai.GatewayRequest, error) {
	resources := pack.Content.Resources
	hidden := false
	request := ai.GatewayRequest{ShowReasoning: &hidden}
	teacher := resources["teaching/teacher"]
	mode := resources["modes/"+input.Mode]
	if input.Teaching == nil {
		request.System = teacher
		for _, rule := range []string{legacyRules, mode} {
			if rule != "" {
				request.System += "\n\n" + rule
			}
		}
		if input.Context != "" {
			request.Messages = append(request.Messages, ai.GatewayMessage{Role: "user", Content: input.Context})
		}
		question := input.Question
		if input.Opening && pack.Opening != "" {
			question = pack.Opening
		}
		request.Messages = append(request.Messages, ai.GatewayMessage{Role: "user", Content: question})
		return request, nil
	}

	turn := input.Teaching
	invalid := errors.New("invalid composed teaching input")
	if err := json.Unmarshal(turn.Messages, &request.Messages); err != nil || len(request.Messages) > 128 {
		return request, invalid
	}
	for _, message := range request.Messages {
		if (message.Role != "system" && message.Role != "user" && message.Role != "assistant") || len(message.ToolCalls) != 0 ||
			message.ToolCallID != "" || len(message.ThinkingBlocks) != 0 || message.ProviderContent != "" || message.ReasoningContent != "" {
			return request, invalid
		}
	}
	if len(turn.Tools) > 0 {
		if err := json.Unmarshal(turn.Tools, &request.Tools); err != nil || len(request.Tools) > 6 {
			return request, invalid
		}
		seen := make(map[string]bool, len(request.Tools))
		for _, tool := range request.Tools {
			switch tool.Name {
			case "find_learning_visual", "search_web", "list_source_documents", "get_source_outline", "search_source", "read_source":
				if seen[tool.Name] || tool.Schema == nil {
					return request, invalid
				}
				seen[tool.Name] = true
			default:
				return request, invalid
			}
		}
	}
	validSlot := func(index int, role string) bool {
		return index >= 0 && index < len(request.Messages) && request.Messages[index].Role == role
	}
	if !validSlot(turn.TeacherIndex, "system") {
		return request, invalid
	}
	request.Messages[turn.TeacherIndex].Content = teacher
	if index := turn.ModeIndex; index != nil {
		if !validSlot(*index, "user") || mode == "" || turn.ModeText == "" || strings.Count(request.Messages[*index].Content, turn.ModeText) != 1 {
			return request, invalid
		}
		message := &request.Messages[*index]
		message.Content = strings.Replace(message.Content, turn.ModeText, mode, 1)
	}
	if index := turn.OpeningIndex; index != nil {
		if !input.Opening || !validSlot(*index, "user") || request.Messages[*index].Content != input.Question || (turn.ModeIndex != nil && *index == *turn.ModeIndex) {
			return request, invalid
		}
		if pack.Opening != "" {
			request.Messages[*index].Content = pack.Opening
		}
	} else if input.Opening {
		return request, invalid
	}
	request.ReasoningEffort = turn.ReasoningEffort
	return request, nil
}

type comparisonToolResult struct {
	ID, Text string
	Final    bool
}

type comparisonToolWait struct {
	subject string
	result  chan comparisonToolResult
}

func (s *Server) submitComparisonToolResult(w http.ResponseWriter, r *http.Request) {
	subject, _, ok := s.comparisonSubject(w, r)
	if !ok {
		return
	}
	var input comparisonToolResult
	if !decode(w, r, &input, 128<<10) {
		return
	}
	if !identifier.MatchString(input.ID) || len(input.Text) > 32<<10 || strings.TrimSpace(input.Text) == "" {
		fail(w, http.StatusBadRequest, "invalid_comparison_tool_result")
		return
	}
	value, found := s.comparisonTools.Load(input.ID)
	if !found || value.(*comparisonToolWait).subject != subject || !s.comparisonTools.CompareAndDelete(input.ID, value) {
		fail(w, http.StatusNotFound, "comparison_tool_unavailable")
		return
	}
	value.(*comparisonToolWait).result <- input
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) requestComparisonTool(ctx context.Context, subject string, call ai.ToolCall, room int, emit func(any)) (comparisonToolResult, error) {
	id := idgen.New("comparison-tool")
	wait := &comparisonToolWait{subject: subject, result: make(chan comparisonToolResult, 1)}
	s.comparisonTools.Store(id, wait)
	defer s.comparisonTools.Delete(id)
	emit(struct {
		Type     string      `json:"type"`
		ResultID string      `json:"resultId"`
		ToolCall ai.ToolCall `json:"toolCall"`
		Room     int         `json:"room"`
	}{"comparison-tool-call", id, call, room})
	select {
	case result := <-wait.result:
		return result, nil
	case <-ctx.Done():
		return comparisonToolResult{}, ctx.Err()
	}
}

func (s *Server) runComparison(ctx context.Context, subject, id, arm, model string, maxInput int, request ai.GatewayRequest, hold store.BudgetHold, start time.Time, emit func(any), forward func(ai.GatewayEvent)) ([]inference.Attempt, error) {
	const maxRounds = 8
	var attempts []inference.Attempt
	preferred := ""
	for round := range maxRounds {
		estimated := (ai.Budget{}).Input(request)
		if estimated > maxInput {
			return attempts, ai.ErrContextBudgetExhausted
		}
		if round > 0 && hold.Limit > 0 {
			estimator := s.gateway.(interface {
				EstimateCost(string, string, int, int, bool) (int64, bool)
			})
			var known bool
			hold.Estimated, known = estimator.EstimateCost(model, hold.Currency, estimated, request.MaxOutputTokens, true)
			if !known {
				return attempts, errors.New("comparison tool continuation cost unavailable")
			}
			if err := s.db.ExtendComparisonArmBudget(ctx, id, arm, start, hold); err != nil {
				return attempts, err
			}
		}
		var text, content, reasoning strings.Builder
		assistant := ai.GatewayMessage{Role: "assistant"}
		collect := func(event ai.GatewayEvent) {
			switch event.Type {
			case "text-delta":
				text.WriteString(event.Delta)
			case "provider-content-delta":
				content.WriteString(event.Delta)
			case "provider-reasoning-delta":
				reasoning.WriteString(event.Delta)
			case "provider-thinking-block":
				assistant.ThinkingBlocks = append(assistant.ThinkingBlocks, append(json.RawMessage(nil), event.ThinkingBlock...))
			case "tool-call-ready":
				if event.ToolCall != nil {
					call := *event.ToolCall
					if call.ProviderCallID == "" {
						call.ProviderCallID = call.CallID
					}
					assistant.ToolCalls = append(assistant.ToolCalls, call)
				}
			}
			forward(event)
		}
		var current []inference.Attempt
		var err error
		if gateway, ok := s.gateway.(interface {
			StreamComparison(context.Context, string, int, string, ai.GatewayRequest, func(ai.GatewayEvent)) ([]inference.Attempt, error)
		}); ok {
			current, err = gateway.StreamComparison(ctx, model, estimated+request.MaxOutputTokens, preferred, request, collect)
		} else {
			current, err = s.gateway.Stream(ctx, model, estimated+request.MaxOutputTokens, preferred, request, collect)
		}
		attempts = append(attempts, current...)
		if err != nil || len(assistant.ToolCalls) == 0 {
			return attempts, err
		}
		if n := len(current); n > 0 {
			preferred = current[n-1].ConnectionID
		}
		if len(assistant.ToolCalls) > 8 || round == maxRounds-1 {
			emit(map[string]string{"type": "comparison-reset"})
			return attempts, errors.New("comparison tool limit reached")
		}
		for _, call := range assistant.ToolCalls {
			allowed := false
			for _, tool := range request.Tools {
				allowed = allowed || tool.Name == call.ToolName
			}
			if !allowed || call.ProviderCallID == "" {
				emit(map[string]string{"type": "comparison-reset"})
				return attempts, errors.New("comparison received an unavailable tool call")
			}
		}
		assistant.Content, assistant.ProviderContent, assistant.ReasoningContent = text.String(), content.String(), reasoning.String()
		request.Messages = append(request.Messages, assistant)
		forceFinal := round == maxRounds-2
		for _, call := range assistant.ToolCalls {
			room := maxInput - (ai.Budget{}).Input(request) - 256*len(assistant.ToolCalls) - 512
			result, err := s.requestComparisonTool(ctx, subject, call, max(0, min(4_000, room)), emit)
			if err != nil {
				return attempts, err
			}
			request.Messages = append(request.Messages, ai.GatewayMessage{Role: "tool", ToolCallID: call.ProviderCallID, Content: result.Text})
			forceFinal = forceFinal || result.Final
		}
		if forceFinal {
			request.Tools = nil
			request.TurnInstructions = s.comparisonToolsFinished
		}
	}
	return attempts, errors.New("comparison tool limit reached")
}
