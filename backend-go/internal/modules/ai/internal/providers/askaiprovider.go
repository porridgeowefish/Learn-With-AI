// Package askaiprovider implements OpenAI-compatible and Anthropic streaming +
// non-streaming chat clients, normalized to one frame shape.
package askaiprovider

import (
	"context"
	"fmt"
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
	switch p.Kind {
	case "anthropic":
		return streamAnthropic(ctx, p, system, msgs, onFrame)
	default:
		return streamOpenAI(ctx, p, system, msgs, onFrame)
	}
}

// Complete returns the full assistant text (non-streaming), for summaries.
func Complete(ctx context.Context, p Provider, system string, msgs []Message) (string, error) {
	switch p.Kind {
	case "anthropic":
		return completeAnthropic(ctx, p, system, msgs)
	default:
		return completeOpenAI(ctx, p, system, msgs)
	}
}

// asHTTPProvider is unused here but reserved for future injection points.
var _ = fmt.Sprint
