// Package askaiprovider implements OpenAI-compatible and Anthropic streaming +
// non-streaming chat clients, normalized to one frame shape.
package askaiprovider

import (
	"context"
	config "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	gateway "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/gateway"
)

// Provider is the connection config (mirrors askaiconfig.Provider).
type Provider struct {
	ID        string
	Kind      string // "openai" | "anthropic"
	Name      string
	BaseURL   string
	APIKey    string
	Model     string
	Reasoning bool
	Thinking  bool
}

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"` // "user" | "assistant" | "system"
	Content string `json:"content"`
}

// Frame is the normalized streaming unit sent to the client.
type Frame struct {
	Type    string `json:"type"` // "text" | "thinking" | "done" | "error"
	Content string `json:"content"`
}

// Stream calls the provider and invokes onFrame for each normalized frame.
func Stream(ctx context.Context, p Provider, system string, msgs []Message, onFrame func(Frame)) error {
	provider := connection(p)
	// Preserve the text API's explicit thinking-display option.
	provider.Reasoning = p.Reasoning || p.Thinking
	return gateway.StreamProvider(ctx, provider, request(system, msgs), func(event gateway.Event) {
		switch event.Type {
		case "text-delta":
			onFrame(Frame{Type: "text", Content: event.Delta})
		case "reasoning-summary-delta":
			if p.Reasoning || p.Thinking {
				onFrame(Frame{Type: "thinking", Content: event.Delta})
			}
		case "response-completed":
			onFrame(Frame{Type: "done"})
		}
	})
}

func Complete(ctx context.Context, p Provider, system string, msgs []Message) (string, error) {
	return gateway.CompleteProvider(ctx, connection(p), request(system, msgs))
}
func connection(p Provider) config.Provider {
	return config.Provider{ID: p.ID, Kind: p.Kind, Name: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.Model, Reasoning: p.Reasoning, Thinking: p.Thinking}
}
func request(system string, msgs []Message) gateway.Request {
	messages := make([]gateway.Message, 0, len(msgs))
	for _, message := range msgs {
		messages = append(messages, gateway.Message{Role: message.Role, Content: message.Content})
	}
	return gateway.Request{System: system, Messages: messages}
}
