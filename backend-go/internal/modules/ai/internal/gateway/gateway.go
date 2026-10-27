package teachergateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	askaiconfig "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
)

// Message is one provider-facing conversation turn. ToolCalls is set on
// assistant messages that carry tool invocations; ToolCallID is set on
// role-"tool" messages answering a specific provider call. Both kinds map to
// the openai tool_calls/role=tool shape and the anthropic tool_use/tool_result
// blocks when present.
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	// Retained only in memory for a provider tool round-trip, never persisted
	// or sent to the browser. Signed/opaque blocks must be replayed intact.
	ThinkingBlocks []json.RawMessage
	// OpenAI-compatible providers can require their full assistant transport
	// content and separate reasoning channel on tool continuations. Neither is
	// persisted nor exposed as learner Markdown.
	ProviderContent  string
	ReasoningContent string
}

type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
}

type ToolCall struct {
	CallID         string
	ProviderCallID string
	ToolName       string
	Arguments      map[string]any
}

type Event struct {
	Type          string
	Delta         string
	ToolCall      *ToolCall
	Usage         map[string]int
	ThinkingBlock json.RawMessage
}

type Request struct {
	System      string
	Messages    []Message
	Tools       []Tool
	ProviderID  string
	BindingName string
	Model       string
	// MaxOutputTokens is a legacy budget annotation, not a provider override.
	// The user-selected platform ModelOutputTokens owns the wire ceiling.
	MaxOutputTokens   int
	MaxThinkingTokens int
}

type Gateway interface {
	Stream(context.Context, Request, func(Event)) error
}

type Configured struct{}

func (Configured) Stream(ctx context.Context, in Request, emit func(Event)) error {
	cfg, err := askaiconfig.Load()
	if err != nil {
		return err
	}
	binding := in.BindingName
	if binding == "" {
		binding = "teacher"
	}
	p := cfg.Resolve(binding)
	if strings.TrimSpace(in.ProviderID) != "" {
		p = cfg.FindExact(strings.TrimSpace(in.ProviderID))
	}
	if p != nil && in.Model != "" {
		p.Model = in.Model
	}
	if p == nil || p.APIKey == "" || p.BaseURL == "" || p.Model == "" {
		return errors.New("teacher provider is not configured")
	}
	return StreamProvider(ctx, *p, in, emit)
}

func streamOpenAI(ctx context.Context, p askaiconfig.Provider, in Request, emit func(Event)) error {
	var buffers toolBufferSet
	var inline inlineThinkingFilter
	body := openAIRequest(p, in, true)
	return streamJSONLines(ctx, strings.TrimRight(p.BaseURL, "/")+"/chat/completions", p, body, func(payload []byte) error {
		if string(payload) == "[DONE]" {
			return io.EOF
		}
		var chunk struct {
			Error   json.RawMessage `json:"error"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ReasoningSummary string `json:"reasoning_summary"`
					Reasoning        string `json:"reasoning"`
					ReasoningText    string `json:"reasoning_text"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return nil
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return errors.New("provider stream error")
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason == "length" {
				emit(Event{Type: "output-limit"})
			}
			emitOutputProgress(emit, choice.Delta.Content+firstNonEmpty(choice.Delta.ReasoningContent, choice.Delta.Reasoning, choice.Delta.ReasoningText, choice.Delta.ReasoningSummary))
			if choice.Delta.ReasoningContent != "" {
				emit(Event{Type: "provider-reasoning-delta", Delta: choice.Delta.ReasoningContent})
			}
			if choice.Delta.ReasoningSummary != "" {
				emit(Event{Type: "reasoning-summary-delta", Delta: choice.Delta.ReasoningSummary})
			} else if p.Reasoning {
				summary := firstNonEmpty(choice.Delta.ReasoningContent, choice.Delta.Reasoning, choice.Delta.ReasoningText)
				if summary != "" {
					emit(Event{Type: "reasoning-summary-delta", Delta: summary})
				}
			}
			if choice.Delta.Content != "" {
				emit(Event{Type: "provider-content-delta", Delta: choice.Delta.Content})
				if visible := inline.add(choice.Delta.Content); visible != "" {
					emit(Event{Type: "text-delta", Delta: visible})
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				emitOutputProgress(emit, call.Function.Arguments)
				buffers.add(call.Index, call.ID, call.Function.Name, call.Function.Arguments)
			}
		}
		if chunk.Usage != nil {
			emit(Event{Type: "usage", Usage: map[string]int{"inputTokens": chunk.Usage.PromptTokens, "outputTokens": chunk.Usage.CompletionTokens}})
		}
		return nil
	}, func() {
		for _, buffered := range buffers.drain() {
			var args map[string]any
			if json.Unmarshal([]byte(buffered.arguments), &args) != nil || args == nil {
				args = map[string]any{"__invalid_arguments": true}
			}
			emit(Event{Type: "tool-call-ready", ToolCall: &ToolCall{CallID: idgen.New("call"), ProviderCallID: buffered.id, ToolName: buffered.name, Arguments: args}})
		}
		emit(Event{Type: "response-completed"})
	})
}

func openAIOutputLimitKey(p askaiconfig.Provider) string {
	return platformconfig.ModelOutputLimitKey(p.BaseURL, p.Model)
}

// The provider stream is processed synchronously, so a request-local buffer is
// kept in the callback closure through this tiny value holder.
type openAIBuffer struct {
	index               int
	id, name, arguments string
}
type toolBufferSet struct{ values map[int]openAIBuffer }

func (s *toolBufferSet) add(index int, id, name, args string) {
	if s.values == nil {
		s.values = map[int]openAIBuffer{}
	}
	v := s.values[index]
	v.index = index
	if id != "" {
		v.id = id
	}
	if name != "" {
		v.name = name
	}
	v.arguments += args
	s.values[index] = v
}
func (s *toolBufferSet) drain() []openAIBuffer {
	var out []openAIBuffer
	indices := make([]int, 0, len(s.values))
	for index := range s.values {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		out = append(out, s.values[index])
	}
	s.values = nil
	return out
}

// openAIMessage maps one gateway message to the openai chat-completions shape,
// including tool_calls and role=tool continuations for the search loop.
func openAIMessage(message Message) map[string]any {
	out := map[string]any{"role": message.Role, "content": message.Content}
	if len(message.ToolCalls) > 0 && message.ProviderContent != "" {
		out["content"] = message.ProviderContent
	}
	if len(message.ToolCalls) > 0 && message.ReasoningContent != "" {
		out["reasoning_content"] = message.ReasoningContent
	}
	if message.ToolCallID != "" {
		out["role"] = "tool"
		out["tool_call_id"] = message.ToolCallID
	}
	if len(message.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			args, _ := json.Marshal(call.Arguments)
			calls = append(calls, map[string]any{"id": call.ProviderCallID, "type": "function", "function": map[string]any{"name": call.ToolName, "arguments": string(args)}})
		}
		out["tool_calls"] = calls
	}
	return out
}

type inlineThinkingFilter struct {
	pending  string
	thinking bool
}

func (f *inlineThinkingFilter) add(delta string) string {
	f.pending += delta
	var visible strings.Builder
	for f.pending != "" {
		if f.thinking {
			at := strings.Index(f.pending, "</think>")
			if at < 0 {
				return visible.String()
			}
			f.pending = f.pending[at+len("</think>"):]
			f.thinking = false
			continue
		}
		at := strings.Index(f.pending, "<think>")
		if at >= 0 {
			visible.WriteString(f.pending[:at])
			f.pending = f.pending[at+len("<think>"):]
			f.thinking = true
			continue
		}
		keep := 0
		for n := 1; n < len("<think>") && n <= len(f.pending); n++ {
			if strings.HasSuffix(f.pending, "<think>"[:n]) {
				keep = n
			}
		}
		visible.WriteString(f.pending[:len(f.pending)-keep])
		f.pending = f.pending[len(f.pending)-keep:]
		break
	}
	return visible.String()
}

// anthropicMessage maps one gateway message to the anthropic messages shape,
// using tool_use/tool_result content blocks for the search loop.
func anthropicMessage(message Message) map[string]any {
	if message.ToolCallID != "" {
		return map[string]any{"role": "user", "content": []map[string]any{{
			"type":        "tool_result",
			"tool_use_id": message.ToolCallID,
			"content":     []map[string]any{{"type": "text", "text": message.Content}},
		}}}
	}
	if len(message.ToolCalls) > 0 {
		blocks := make([]any, 0, len(message.ThinkingBlocks)+len(message.ToolCalls)+1)
		for _, block := range message.ThinkingBlocks {
			blocks = append(blocks, block)
		}
		if message.Content != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": message.Content})
		}
		for _, call := range message.ToolCalls {
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ProviderCallID, "name": call.ToolName, "input": call.Arguments})
		}
		return map[string]any{"role": "assistant", "content": blocks}
	}
	return map[string]any{"role": message.Role, "content": message.Content}
}

func streamAnthropic(ctx context.Context, p askaiconfig.Provider, in Request, emit func(Event)) error {
	body := anthropicRequest(p, in, true)
	var toolBuffers toolBufferSet
	blockTypes := map[int]string{}
	thinkingBlocks := map[int]map[string]any{}
	// shape records the block/delta type fingerprint of this provider stream.
	// GLM's anthropic-compat endpoint intermittently mislabels thinking or
	// body deltas (the 2026-09-12 mixed reasoning incidents); this log pins
	// the raw shape on the next occurrence without capturing any content.
	shape := newStreamShape(p.Model)
	defer shape.flush()
	return streamJSONLines(ctx, strings.TrimRight(p.BaseURL, "/")+"/v1/messages", p, body, func(payload []byte) error {
		var event struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type     string          `json:"type"`
				ID       string          `json:"id"`
				Name     string          `json:"name"`
				Thinking string          `json:"thinking"`
				Input    json.RawMessage `json:"input"`
			} `json:"content_block"`
			Delta struct {
				StopReason  string `json:"stop_reason"`
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				Summary     string `json:"summary"`
				PartialJSON string `json:"partial_json"`
				Signature   string `json:"signature"`
			} `json:"delta"`
			Message struct {
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(payload, &event) != nil {
			return nil
		}
		switch event.Type {
		case "error":
			return errors.New("provider stream error")
		case "message_start":
			// message_start carries usage nested inside its message envelope;
			// input token counts only exist there.
			if event.Message.Usage != nil {
				emit(Event{Type: "usage", Usage: map[string]int{"inputTokens": event.Message.Usage.InputTokens}})
			}
		case "content_block_start":
			blockTypes[event.Index] = event.ContentBlock.Type
			if event.ContentBlock.Type == "thinking" || event.ContentBlock.Type == "redacted_thinking" {
				var envelope struct {
					Block map[string]any `json:"content_block"`
				}
				if json.Unmarshal(payload, &envelope) == nil {
					thinkingBlocks[event.Index] = envelope.Block
				}
			}
			shape.blockStart(event.Index, event.ContentBlock.Type)
			if event.ContentBlock.Type == "tool_use" {
				toolBuffers.add(event.Index, event.ContentBlock.ID, event.ContentBlock.Name, "")
				if len(event.ContentBlock.Input) > 0 && string(event.ContentBlock.Input) != "{}" {
					toolBuffers.add(event.Index, "", "", string(event.ContentBlock.Input))
				}
			} else if p.Reasoning && event.ContentBlock.Type == "thinking" && event.ContentBlock.Thinking != "" {
				emit(Event{Type: "reasoning-summary-delta", Delta: event.ContentBlock.Thinking})
			}
		case "content_block_delta":
			emitOutputProgress(emit, firstNonEmpty(event.Delta.Text, event.Delta.Thinking, event.Delta.Summary, event.Delta.PartialJSON))
			shape.delta(event.Index, event.Delta.Type)
			switch event.Delta.Type {
			case "text_delta":
				if blockTypes[event.Index] == "thinking" {
					appendThinkingField(thinkingBlocks[event.Index], "thinking", event.Delta.Text)
					if p.Reasoning && event.Delta.Text != "" {
						emit(Event{Type: "reasoning-summary-delta", Delta: event.Delta.Text})
					}
				} else {
					emit(Event{Type: "text-delta", Delta: event.Delta.Text})
				}
			case "summary_delta":
				// Delta semantics are stricter than a provider's outer block label:
				// a summary is never learner-answer Markdown.
				if event.Delta.Summary != "" {
					emit(Event{Type: "reasoning-summary-delta", Delta: event.Delta.Summary})
				}
			case "thinking_delta":
				appendThinkingField(thinkingBlocks[event.Index], "thinking", event.Delta.Thinking)
				// GLM's anthropic-compatible stream has emitted thinking deltas in
				// text blocks. Never let that label error mix reasoning into body.
				if p.Reasoning && event.Delta.Thinking != "" {
					emit(Event{Type: "reasoning-summary-delta", Delta: event.Delta.Thinking})
				}
			case "input_json_delta":
				toolBuffers.add(event.Index, "", "", event.Delta.PartialJSON)
			case "signature_delta":
				appendThinkingField(thinkingBlocks[event.Index], "signature", event.Delta.Signature)
			}
		case "content_block_stop":
			if block, ok := thinkingBlocks[event.Index]; ok {
				raw, err := json.Marshal(block)
				if err != nil {
					return err
				}
				emit(Event{Type: "provider-thinking-block", ThinkingBlock: raw})
				delete(thinkingBlocks, event.Index)
			}
			delete(blockTypes, event.Index)
			if buffered, ok := toolBuffers.values[event.Index]; ok && buffered.name != "" {
				var args map[string]any
				if buffered.arguments == "" {
					buffered.arguments = "{}"
				}
				if json.Unmarshal([]byte(buffered.arguments), &args) != nil || args == nil {
					args = map[string]any{"__invalid_arguments": true}
				}
				emit(Event{Type: "tool-call-ready", ToolCall: &ToolCall{CallID: idgen.New("call"), ProviderCallID: buffered.id, ToolName: buffered.name, Arguments: args}})
				delete(toolBuffers.values, event.Index)
			}
		case "message_delta":
			if event.Delta.StopReason == "max_tokens" {
				emit(Event{Type: "output-limit"})
			}
			if event.Usage != nil {
				emit(Event{Type: "usage", Usage: map[string]int{"outputTokens": event.Usage.OutputTokens}})
			}
		case "message_stop":
			emit(Event{Type: "response-completed"})
			return io.EOF
		}
		return nil
	}, nil)
}

func appendThinkingField(block map[string]any, field, delta string) {
	if block == nil || delta == "" {
		return
	}
	current, _ := block[field].(string)
	block[field] = current + delta
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Progress exposes counts, never reasoning text. It also works when reasoning
// summaries are disabled, and counts tool argument output exactly once.
func emitOutputProgress(emit func(Event), text string) {
	if text != "" {
		emit(Event{Type: "output-progress", Usage: map[string]int{"characters": len([]rune(text))}})
	}
}

func streamJSONLines(ctx context.Context, url string, p askaiconfig.Provider, body any, onData func([]byte) error, onEnd func()) error {
	resp, err := requestProvider(ctx, url, p, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if err := onData([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))); err != nil {
			if errors.Is(err, io.EOF) {
				if onEnd != nil {
					onEnd()
				}
				return nil
			}
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

// streamShape fingerprints one anthropic stream's block/delta labeling so
// provider-side mislabeling (thinking content routed to the 正文 channel on
// 2026-09-12) leaves hard evidence in tmp/teacher-stream-shapes.log. It
// records types and counts only — never message content.
type streamShape struct {
	model  string
	blocks []string
	deltas []string
	total  int
}

func newStreamShape(model string) *streamShape { return &streamShape{model: model} }

func (s *streamShape) blockStart(index int, blockType string) {
	s.blocks = append(s.blocks, fmt.Sprintf("%d:%s", index, blockType))
}

func (s *streamShape) delta(index int, deltaType string) {
	s.total++
	s.deltas = append(s.deltas, fmt.Sprintf("%d:%s", index, deltaType))
}

func (s *streamShape) fingerprint() string {
	counts := map[string]int{}
	for _, d := range s.deltas {
		counts[d]++
	}
	pairs := make([]string, 0, len(counts))
	for _, d := range s.deltas { // preserve first-seen order deterministically
		if counts[d] > 0 {
			pairs = append(pairs, fmt.Sprintf("%s×%d", d, counts[d]))
			counts[d] = 0
		}
	}
	return "blocks=[" + strings.Join(s.blocks, ",") + "] deltas=[" + strings.Join(pairs, ",") + "]"
}

func (s *streamShape) flush() {
	if s.total == 0 {
		return
	}
	path := filepath.Join(paths.WORKSPACE, "tmp", "teacher-stream-shapes.log")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s model=%s %s\n", time.Now().UTC().Format(time.RFC3339), s.model, s.fingerprint())
}
