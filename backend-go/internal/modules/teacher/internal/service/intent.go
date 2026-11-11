package teacherservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
)

var intentPrompt = promptText("auxiliary/tool-intent")

type toolIntent struct {
	EndDecision string `json:"endDecision"`
	Evidence    string `json:"evidence"`
	Required    []struct {
		Tool  string `json:"tool"`
		Query string `json:"query"`
		Kind  string `json:"kind"`
	} `json:"required"`
	Forbidden []string `json:"forbidden"`
}

func (s *Service) inferToolIntent(ctx context.Context, providerID, model, learnerText string, history []ai.GatewayMessage, recordCredit func(*ai.CreditSettlement)) (toolIntent, map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	intentPrompt, promptErr := ai.RenderPrompt("auxiliary/tool-intent", nil)
	if promptErr != nil {
		return toolIntent{}, nil, promptErr
	}
	var recent []ai.GatewayMessage
	// Prompt references follow the transcript in the teacher request.
	// Only actual conversation turns should resolve the learner's intent.
	for len(history) > 0 && history[len(history)-1].Pinned {
		history = history[:len(history)-1]
	}
	for i := max(0, len(history)-4); i < len(history); i++ {
		m := history[i]
		if !m.Pinned && (m.Role == "user" || m.Role == "assistant") && m.Content != "" {
			content := m.Content
			if m.Role == "assistant" {
				runes := []rune(content)
				if len(runes) > 1200 {
					content = ai.TruncateTokens(content, 300) + "\n…\n" + string(runes[len(runes)-600:])
				}
			} else {
				content = ai.TruncateTokens(content, 300)
			}
			recent = append(recent, ai.GatewayMessage{Role: m.Role, Content: content})
		}
	}
	recent = append(recent, ai.GatewayMessage{Role: "user", Content: "判断这一条学习者请求（最近消息只用于消解指代）：\n" + ai.TruncateTokens(learnerText, 1200)})
	var out strings.Builder
	usage := map[string]int{}
	hidden := false
	err := s.Gateway.Stream(ctx, ai.GatewayRequest{System: intentPrompt, Messages: recent, ProviderID: providerID, Model: model, ShowReasoning: &hidden, MaxOutputTokens: 512}, func(event ai.GatewayEvent) {
		if event.Type == "text-delta" {
			if out.Len()+len(event.Delta) > 4096 {
				cancel()
				return
			}
			out.WriteString(event.Delta)
		}
		if event.Type == "usage" {
			for k, v := range event.Usage {
				usage[k] = v
			}
		}
		if event.Type == "credit-settlement" && recordCredit != nil {
			recordCredit(event.CreditSettlement)
		}
	})
	if err != nil {
		return toolIntent{}, usage, err
	}
	if ctx.Err() != nil {
		return toolIntent{}, usage, ctx.Err()
	}
	plan, err := parseToolIntent(out.String())
	return plan, usage, err
}

func parseToolIntent(raw string) (toolIntent, error) {
	var plan toolIntent
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```json") && strings.HasSuffix(raw, "```") {
		raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "```json"), "```"))
	}
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return plan, err
	}
	if plan.Required == nil || plan.Forbidden == nil || len(plan.Required) > 2 || len(plan.Forbidden) > 4 {
		return toolIntent{}, errors.New("invalid tool intent")
	}
	if plan.EndDecision != "" && plan.EndDecision != "confirm_end" && plan.EndDecision != "continue" && plan.EndDecision != "unclear" {
		return toolIntent{}, errors.New("invalid teaching end decision")
	}
	for _, kind := range plan.Forbidden {
		if kind != "all" && kind != "web" && kind != "visual" && kind != "sources" {
			return toolIntent{}, errors.New("invalid tool prohibition")
		}
	}
	for _, action := range plan.Required {
		limit := 120
		switch action.Tool {
		case "search_web":
			limit = maxSearchQueryRunes
		case "find_learning_visual":
			if action.Kind != "diagram" && action.Kind != "molecule" && action.Kind != "manufacturing" {
				return toolIntent{}, errors.New("invalid visual intent")
			}
		case "search_source":
		case "list_source_documents":
			continue
		default:
			return toolIntent{}, errors.New("invalid intent tool")
		}
		if strings.TrimSpace(action.Query) == "" || len([]rune(action.Query)) > limit {
			return toolIntent{}, errors.New("invalid intent query")
		}
	}
	return plan, nil
}

func (plan toolIntent) allows(name string) bool {
	for _, kind := range plan.Forbidden {
		if kind == "all" || (kind == "web" && (name == "search_web" || name == "find_learning_visual")) || (kind == "visual" && name == "find_learning_visual") || (kind == "sources" && isSourceTool(name)) {
			return false
		}
	}
	return true
}

func (plan toolIntent) calls(tools []ai.Tool) ([]ai.ToolCall, []string) {
	var calls []ai.ToolCall
	var unavailable []string
	seen := map[string]bool{}
	for _, action := range plan.Required {
		key := action.Tool + "\x00" + action.Kind + "\x00" + action.Query
		if !plan.allows(action.Tool) || seen[key] {
			continue
		}
		seen[key] = true
		offered := false
		for _, tool := range tools {
			if tool.Name == action.Tool {
				offered = true
				break
			}
		}
		if !offered {
			unavailable = append(unavailable, action.Tool)
			continue
		}
		args := map[string]any{}
		switch action.Tool {
		case "search_source":
			args["queries"] = []any{action.Query}
		case "list_source_documents":
		default:
			args["query"] = action.Query
		}
		if action.Tool == "find_learning_visual" {
			args["kind"] = action.Kind
		}
		id := idgen.New("call")
		calls = append(calls, ai.ToolCall{CallID: id, ProviderCallID: id, ToolName: action.Tool, Arguments: args})
	}
	return calls, unavailable
}
