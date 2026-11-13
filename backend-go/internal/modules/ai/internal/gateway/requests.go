package teachergateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	askaiconfig "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

func messagesWithTurnInstructions(in Request) []Message {
	if in.TurnInstructions == "" {
		return in.Messages
	}
	i := 0
	for i < len(in.Messages) && in.Messages[i].Role == "system" {
		i++
	}
	return slices.Concat(in.Messages[:i], []Message{{Role: "system", Content: in.TurnInstructions}}, in.Messages[i:])
}

func openAIRequest(p askaiconfig.Provider, in Request, stream bool) map[string]any {
	messages := make([]map[string]any, 0, len(in.Messages)+1)
	if in.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": in.System})
	}
	for _, message := range messagesWithTurnInstructions(in) {
		messages = append(messages, openAIMessage(message))
	}
	body := map[string]any{"model": p.Model, "messages": messages, "stream": stream}
	if in.ReasoningEffort != "" {
		body["reasoning_effort"] = in.ReasoningEffort
	}
	body[openAIOutputLimitKey(p)] = outputLimit(in)
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
	// Anthropic accepts fixed instructions only in its top-level system field.
	// Preserve instruction order while projecting ordered composition slots;
	// reference data remains user content and is never promoted with them.
	var systemParts []string
	if in.System != "" {
		systemParts = append(systemParts, in.System)
	}
	previousTool := false
	for _, message := range in.Messages {
		if message.Role == "system" {
			systemParts = append(systemParts, message.Content)
			continue
		}
		mapped := anthropicMessage(message)
		if message.ToolCallID != "" && previousTool && len(messages) > 0 {
			last := messages[len(messages)-1]
			last["content"] = append(last["content"].([]map[string]any), mapped["content"].([]map[string]any)...)
		} else {
			messages = append(messages, mapped)
		}
		previousTool = message.ToolCallID != ""
	}
	system := strings.Join(systemParts, "\n\n")
	body := map[string]any{"model": p.Model, "system": system, "messages": messages, "stream": stream, "max_tokens": outputLimit(in)}
	if in.TurnInstructions != "" {
		if system == "" {
			body["system"] = in.TurnInstructions
		} else {
			body["system"] = system + "\n\n" + in.TurnInstructions
		}
	}
	// Explicit caching is a first-party Anthropic capability. Compatible
	// providers (including Coding Plan endpoints) retain their accepted schema.
	if endpoint, err := url.Parse(p.BaseURL); err == nil && endpoint.Scheme == "https" && strings.EqualFold(endpoint.Hostname(), "api.anthropic.com") && system != "" {
		blocks := []map[string]any{{"type": "text", "text": system, "cache_control": map[string]any{"type": "ephemeral"}}}
		if in.TurnInstructions != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": "\n\n" + in.TurnInstructions})
		}
		body["system"] = blocks
	}
	// Merely offering tools must not change the configured thinking channel.
	// Older externally supplied tool history lacking signed blocks still uses
	// the non-thinking request shape; live tool rounds retain them in memory.
	replayableThinking := true
	for _, message := range in.Messages {
		if len(message.ToolCalls) > 0 && len(message.ThinkingBlocks) == 0 {
			replayableThinking = false
		}
	}
	if (p.Thinking || in.ReasoningEffort != "") && replayableThinking {
		thinkingBudget := 2048
		if in.MaxThinkingTokens > 0 {
			thinkingBudget = in.MaxThinkingTokens
		}
		if in.ReasoningEffort != "" {
			thinkingBudget = map[string]int{"low": 2048, "medium": 4096, "high": 8192, "xhigh": 16384}[in.ReasoningEffort]
		}
		if ceiling := outputLimit(in); ceiling > 1024 {
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": min(thinkingBudget, ceiling-1)}
		}
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
	client := http.DefaultClient
	if inManagedStream(ctx) {
		client = noRedirectClient(client)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, rateLimitError(resp)
		}
		if inManagedStream(ctx) && upstreamAuthRejected(resp) {
			return nil, ErrUpstreamAuthRejected
		}
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if contextLimitResponse(detail) {
			return nil, ErrContextOverflow
		}
		return nil, &UpstreamHTTPError{Status: resp.StatusCode, Detail: strings.TrimSpace(string(detail))}
	}
	return resp, nil
}

// UpstreamRateLimit denotes an HTTP rejection before any response stream.
type UpstreamRateLimit struct{ RetryAfter time.Duration }

type UpstreamHTTPError struct {
	Status int
	Detail string
}

func (e *UpstreamHTTPError) Error() string {
	return fmt.Sprintf("provider status %d: %s", e.Status, e.Detail)
}

var ErrUpstreamAuthRejected = errors.New("upstream authentication rejected")

func upstreamAuthRejected(response *http.Response) bool {
	if response.StatusCode == http.StatusUnauthorized {
		return true
	}
	// A bare 403 can describe a provider policy decision instead of a bad key.
	return response.StatusCode == http.StatusForbidden &&
		strings.Contains(strings.ToLower(response.Header.Get("WWW-Authenticate")), `error="invalid_token"`)
}

func (*UpstreamRateLimit) Error() string { return "provider status 429: rate limited" }

func rateLimitError(resp *http.Response) *UpstreamRateLimit {
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	var delay time.Duration
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	} else if until, err := http.ParseTime(value); err == nil {
		delay = max(0, time.Until(until))
	}
	return &UpstreamRateLimit{RetryAfter: delay}
}

// StreamProvider uses the same protocol implementation for configured and explicit connections.
func StreamProvider(ctx context.Context, p askaiconfig.Provider, in Request, emit func(Event)) error {
	release, err := acquireProvider(ctx, p)
	if err != nil {
		return err
	}
	defer release()
	return streamUpstream(ctx, p, in, emit)
}

// StreamUpstream sends an already admitted request using the provider protocol.
func StreamUpstream(ctx context.Context, p askaiconfig.Provider, in Request, emit func(Event)) error {
	in.upstreamOutputLimit = in.MaxOutputTokens
	return streamUpstream(context.WithValue(ctx, managedStreamKey{}, true), p, in, emit)
}

type managedStreamKey struct{}

func inManagedStream(ctx context.Context) bool {
	value, _ := ctx.Value(managedStreamKey{}).(bool)
	return value
}

func noRedirectClient(base *http.Client) *http.Client {
	return &http.Client{
		Transport: base.Transport, Timeout: base.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func outputLimit(in Request) int {
	if in.upstreamOutputLimit > 0 {
		return in.upstreamOutputLimit
	}
	return platformconfig.ModelOutputTokens
}

func streamUpstream(ctx context.Context, p askaiconfig.Provider, in Request, emit func(Event)) error {
	if err := validateEffort(p, in); err != nil {
		return err
	}
	if in.ShowReasoning != nil {
		p.Reasoning = *in.ShowReasoning
	}
	if p.Kind == "siwc" {
		return streamSIWC(ctx, p, in, emit)
	}
	if usesOpenAIResponses(p) {
		return streamResponses(ctx, p, in, emit)
	}
	if p.Kind == "anthropic" {
		return streamAnthropic(ctx, p, in, emit)
	}
	return streamOpenAI(ctx, p, in, emit)
}

// CompleteProvider shares request mapping and transport with streaming calls.
func CompleteProvider(ctx context.Context, p askaiconfig.Provider, in Request) (string, error) {
	release, admissionErr := acquireProvider(ctx, p)
	if admissionErr != nil {
		return "", admissionErr
	}
	defer release()
	if p.Kind == "siwc" || usesOpenAIResponses(p) {
		var out strings.Builder
		stream := streamSIWC
		if usesOpenAIResponses(p) {
			stream = streamResponses
		}
		err := stream(ctx, p, in, func(event Event) {
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
