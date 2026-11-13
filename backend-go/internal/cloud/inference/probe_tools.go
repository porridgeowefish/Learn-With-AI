package inference

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
)

const probeToolName = "lll_probe_echo"

// ProbeTools checks a synthetic tool call and its result on the same connection.
// The gateway supplies a fixed result without executing any tool.
func (g *Gateway) ProbeTools(ctx context.Context, connectionID string) (ProbeResult, error) {
	upstream, ok := g.providers[connectionID]
	if !ok {
		return ProbeResult{}, errors.New("unknown upstream connection")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	request := ai.GatewayRequest{
		Messages: []ai.GatewayMessage{{Role: "user", Content: "Call lll_probe_echo once with value ping. After receiving its result, reply with OK."}},
		Tools: []ai.Tool{{Name: probeToolName, Description: "Echo a diagnostic value.", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"value": map[string]string{"type": "string"}}, "required": []string{"value"},
		}}},
		// Reasoning providers need room for both reasoning and tool arguments.
		MaxOutputTokens: probeOutputLimit(upstream, 512),
	}
	result := ProbeResult{Model: upstream.Provider.Model}
	assistant := ai.GatewayMessage{Role: "assistant"}
	var content, providerContent, reasoning strings.Builder
	err := ai.StreamUpstream(ctx, g.client, upstream.Provider, request, func(event ai.GatewayEvent) {
		switch event.Type {
		case "tool-call-ready":
			if event.ToolCall != nil {
				assistant.ToolCalls = append(assistant.ToolCalls, *event.ToolCall)
			}
		case "text-delta":
			content.WriteString(event.Delta)
		case "provider-content-delta":
			providerContent.WriteString(event.Delta)
		case "provider-reasoning-delta":
			reasoning.WriteString(event.Delta)
		case "provider-thinking-block":
			assistant.ThinkingBlocks = append(assistant.ThinkingBlocks, event.ThinkingBlock)
		}
	})
	if result.Status = probeStreamError(err); result.Status != "" {
		return result, nil
	}
	if len(assistant.ToolCalls) != 1 {
		result.Status = "tool_call_missing"
		return result, nil
	}
	call := assistant.ToolCalls[0]
	value, _ := call.Arguments["value"].(string)
	if call.ProviderCallID == "" || call.ToolName != probeToolName || value != "ping" {
		result.Status = "tool_call_invalid"
		return result, nil
	}
	assistant.Content = content.String()
	assistant.ProviderContent = providerContent.String()
	assistant.ReasoningContent = reasoning.String()
	request.Messages = append(request.Messages, assistant, ai.GatewayMessage{Role: "tool", ToolCallID: call.ProviderCallID, Content: `{"value":"ping"}`})
	seenText := false
	err = ai.StreamUpstream(ctx, g.client, upstream.Provider, request, func(event ai.GatewayEvent) {
		if event.Type == "text-delta" && strings.TrimSpace(event.Delta) != "" {
			seenText = true
		}
	})
	if result.Status = probeStreamError(err); result.Status != "" {
		return result, nil
	}
	result.Status = "tool_reply_missing"
	if seenText {
		result.Status = "verified"
	}
	return result, nil
}
