package teachergateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	askaiconfig "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
	"io"
	"net/http"
	"strings"
)

func openAIRequest(p askaiconfig.Provider, in Request, stream bool) map[string]any {
	messages := make([]map[string]any, 0, len(in.Messages)+1)
	if in.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": in.System})
	}
	for _, message := range in.Messages {
		messages = append(messages, openAIMessage(message))
	}
	body := map[string]any{"model": p.Model, "messages": messages, "stream": stream}
	body[openAIOutputLimitKey(p)] = platformconfig.ModelOutputTokens
	if len(in.Tools) > 0 {
		var tools []map[string]any
		for _, tool := range in.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Schema}})
		}
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	if stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	return body
}

func anthropicRequest(p askaiconfig.Provider, in Request, stream bool) map[string]any {
	var messages []map[string]any
	previousTool := false
	for _, message := range in.Messages {
		mapped := anthropicMessage(message)
		if message.ToolCallID != "" && previousTool && len(messages) > 0 {
			last := messages[len(messages)-1]
			last["content"] = append(last["content"].([]map[string]any), mapped["content"].([]map[string]any)...)
		} else {
			messages = append(messages, mapped)
		}
		previousTool = message.ToolCallID != ""
	}
	body := map[string]any{"model": p.Model, "system": in.System, "messages": messages, "stream": stream, "max_tokens": platformconfig.ModelOutputTokens}
	// Merely offering tools must not change the configured thinking channel.
	// Older externally supplied tool history lacking signed blocks still uses
	// the non-thinking request shape; live tool rounds retain them in memory.
	replayableThinking := true
	for _, message := range in.Messages {
		if len(message.ToolCalls) > 0 && len(message.ThinkingBlocks) == 0 {
			replayableThinking = false
		}
	}
	if p.Thinking && replayableThinking {
		thinkingBudget := 2048
		if in.MaxThinkingTokens > 0 {
			thinkingBudget = in.MaxThinkingTokens
		}
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": thinkingBudget}
	}
	if len(in.Tools) > 0 {
		var tools []map[string]any
		for _, tool := range in.Tools {
			tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.Schema})
		}
		body["tools"] = tools
	}
	return body
}

func requestProvider(ctx context.Context, url string, p askaiconfig.Provider, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Kind == "anthropic" {
		req.Header.Set("x-api-key", p.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("provider status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return resp, nil
}

// StreamProvider uses the same protocol implementation for configured and explicit connections.
func StreamProvider(ctx context.Context, p askaiconfig.Provider, in Request, emit func(Event)) error {
	if p.Kind == "siwc" {
		return streamSIWC(ctx, p, in, emit)
	}
	if p.Kind == "anthropic" {
		return streamAnthropic(ctx, p, in, emit)
	}
	return streamOpenAI(ctx, p, in, emit)
}

// CompleteProvider shares request mapping and transport with streaming calls.
func CompleteProvider(ctx context.Context, p askaiconfig.Provider, in Request) (string, error) {
	if p.Kind == "siwc" {
		var out strings.Builder
		err := streamSIWC(ctx, p, in, func(event Event) {
			if event.Type == "text-delta" {
				out.WriteString(event.Delta)
			}
		})
		return out.String(), err
	}
	p.Thinking = false
	body := openAIRequest(p, in, false)
	endpoint := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	if p.Kind == "anthropic" {
		body = anthropicRequest(p, in, false)
		endpoint = strings.TrimRight(p.BaseURL, "/") + "/v1/messages"
	}
	response, err := requestProvider(ctx, endpoint, p, body)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Error) > 0 && string(result.Error) != "null" {
		return "", fmt.Errorf("provider response error")
	}
	if p.Kind != "anthropic" {
		if len(result.Choices) > 0 {
			return result.Choices[0].Message.Content, nil
		}
		return "", nil
	}
	var out strings.Builder
	for _, block := range result.Content {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	return out.String(), nil
}
