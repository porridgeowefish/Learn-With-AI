package teachergateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	askaiconfig "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	"github.com/xmz14/lll/backend-go/internal/platform/netproxy"
	"github.com/xmz14/lll/backend-go/internal/platform/siwc"
)

var siwcAccessToken = siwc.AccessToken
var siwcHTTPClient = &http.Client{Transport: netproxy.Transport()}

func streamSIWC(ctx context.Context, p askaiconfig.Provider, in Request, emit func(Event)) error {
	p.Kind = "siwc"
	return streamResponses(ctx, p, in, emit)
}
func streamResponses(ctx context.Context, p askaiconfig.Provider, in Request, emit func(Event)) error {
	token := p.APIKey
	endpoint := strings.TrimRight(p.BaseURL, "/") + "/responses"
	if p.Kind == "siwc" {
		var err error
		token, err = siwcAccessToken(ctx)
		if err != nil {
			return err
		}
		endpoint = "https://api.openai.com/v1/responses"
	}
	input := make([]map[string]any, 0, len(in.Messages)*2)
	for _, m := range in.Messages {
		if m.ToolCallID != "" {
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
			continue
		}
		if len(m.ToolCalls) > 0 {
			if m.Content != "" {
				input = append(input, map[string]any{"role": "assistant", "content": m.Content})
			}
			for _, call := range m.ToolCalls {
				args, _ := json.Marshal(call.Arguments)
				input = append(input, map[string]any{"type": "function_call", "call_id": call.ProviderCallID, "name": "lll." + call.ToolName, "arguments": string(args)})
			}
			continue
		}
		role := m.Role
		if role == "system" {
			role = "developer"
		}
		input = append(input, map[string]any{"role": role, "content": m.Content})
	}
	body := map[string]any{"model": p.Model, "input": input, "store": false, "stream": true}
	if p.Kind != "siwc" {
		body["max_output_tokens"] = platformconfig.ModelOutputTokens
	}
	if supportsSIWCReasoningSummary(p.Model) {
		reasoning := map[string]any{}
		if in.ShowReasoning == nil || *in.ShowReasoning {
			reasoning["summary"] = "auto"
		}
		if in.ReasoningEffort != "" {
			reasoning["effort"] = in.ReasoningEffort
		}
		if len(reasoning) > 0 {
			body["reasoning"] = reasoning
		}
	}
	if in.System != "" {
		body["instructions"] = in.System
	}
	if len(in.Tools) > 0 {
		tools := make([]map[string]any, 0, len(in.Tools))
		for _, tool := range in.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": tool.Name, "description": tool.Description, "parameters": tool.Schema})
		}
		body["tools"] = []map[string]any{{"type": "namespace", "name": "lll", "description": "Local learning tools available in Learn-With-AI", "tools": tools}}
	}
	if in.WebSearch {
		body["tools"] = []map[string]any{{"type": "web_search"}}
		body["tool_choice"] = "required"
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := siwcHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("OpenAI request status %d: %s", res.StatusCode, strings.TrimSpace(string(detail)))
	}
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	completed := false
	seenCalls := map[string]bool{}
	searchCompleted := false
	sources := map[string]bool{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var frame struct {
			Annotation struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"annotation"`
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Item  struct {
				Type      string `json:"type"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"item"`
			Response struct {
				Status string
				Error  *struct{ Code, Message string }
				Usage  *responsesUsage `json:"usage"`
				Output []struct {
					Type      string `json:"type"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(payload), &frame) != nil {
			continue
		}
		switch frame.Type {
		case "response.web_search_call.completed":
			searchCompleted = true
		case "response.output_text.annotation.added":
			if in.WebSearch && strings.HasPrefix(frame.Annotation.URL, "https://") && !sources[frame.Annotation.URL] {
				sources[frame.Annotation.URL] = true
				emit(Event{Type: "text-delta", Delta: "\n来源：" + frame.Annotation.URL + "\n"})
			}
		case "response.output_text.delta":
			emit(Event{Type: "text-delta", Delta: frame.Delta})
		case "response.reasoning_summary_text.delta":
			emit(Event{Type: "reasoning-summary-delta", Delta: frame.Delta})
		case "response.output_item.done":
			if frame.Item.Type == "function_call" {
				emitSIWCCall(frame.Item.CallID, frame.Item.Name, frame.Item.Arguments, seenCalls, emit)
			}
		case "response.completed":
			for _, item := range frame.Response.Output {
				if item.Type == "function_call" {
					emitSIWCCall(item.CallID, item.Name, item.Arguments, seenCalls, emit)
				}
			}
			if frame.Response.Usage != nil {
				emit(Event{Type: "usage", Usage: frame.Response.Usage.metrics()})
			}
			emit(Event{Type: "response-completed"})
			completed = true
		case "response.failed", "response.incomplete":
			if frame.Response.Error != nil {
				return fmt.Errorf("OpenAI response %s: %s", frame.Response.Error.Code, frame.Response.Error.Message)
			}
			return fmt.Errorf("OpenAI response %s", frame.Type)
		}
		if completed {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !completed {
		return errors.New("OpenAI stream ended before response.completed")
	}
	if in.WebSearch && !searchCompleted {
		return errors.New("当前连接未完成联网搜索，请核对模型和账户权限")
	}
	return nil
}
func supportsSIWCReasoningSummary(model string) bool {
	if strings.Contains(model, "-chat") || strings.Contains(model, "realtime") {
		return false
	}
	return strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "gpt-6") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4")
}
func emitSIWCCall(id, name, args string, seen map[string]bool, emit func(Event)) {
	if id == "" || seen[id] {
		return
	}
	seen[id] = true
	name = strings.TrimPrefix(name, "lll.")
	var arguments map[string]any
	if json.Unmarshal([]byte(args), &arguments) != nil || arguments == nil {
		arguments = map[string]any{"__invalid_arguments": true}
	}
	emit(Event{Type: "tool-call-ready", ToolCall: &ToolCall{CallID: idgen.New("call"), ProviderCallID: id, ToolName: name, Arguments: arguments}})
}

func usesOpenAIResponses(p askaiconfig.Provider) bool {
	endpoint, err := url.Parse(p.BaseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() != "api.openai.com" {
		return false
	}
	model := strings.ToLower(strings.TrimSpace(p.Model))
	return p.Kind == "openai" && (model == "gpt-6.1-sol" || strings.HasPrefix(model, "gpt-6.1-sol-"))
}
