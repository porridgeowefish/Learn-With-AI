package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	gateway "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/gateway"
)

type Handle func(context.Context, map[string]any) (any, error)
type ToolSpec struct {
	Definition     gateway.Tool
	Handle         Handle
	DescriptionRef string
	Capabilities   []string
	SideEffects    string
	Authorize      func(context.Context) error
	Validate       func(map[string]any) error
}
type Registry struct{ specs map[string]ToolSpec }
type BoundTools struct {
	specs map[string]ToolSpec
	order []string
}

func (r *Registry) Register(s ToolSpec) error {
	if s.Definition.Name == "" || s.Handle == nil {
		return fmt.Errorf("invalid tool registration")
	}
	if r.specs == nil {
		r.specs = map[string]ToolSpec{}
	}
	if _, ok := r.specs[s.Definition.Name]; ok {
		return fmt.Errorf("duplicate tool %s", s.Definition.Name)
	}
	raw, err := json.Marshal(s.Definition)
	if err != nil {
		return err
	}
	json.Unmarshal(raw, &s.Definition)
	r.specs[s.Definition.Name] = s
	return nil
}
func (r *Registry) Bind(names []string) (*BoundTools, error) {
	b := &BoundTools{specs: map[string]ToolSpec{}}
	for _, n := range names {
		s, ok := r.specs[n]
		if !ok {
			return nil, fmt.Errorf("unknown tool %s", n)
		}
		if _, ok := b.specs[n]; ok {
			return nil, fmt.Errorf("duplicate tool %s", n)
		}
		b.specs[n] = s
		b.order = append(b.order, n)
	}
	return b, nil
}
func (b *BoundTools) Definitions() []gateway.Tool {
	out := []gateway.Tool{}
	for _, n := range b.order {
		raw, _ := json.Marshal(b.specs[n].Definition)
		var d gateway.Tool
		json.Unmarshal(raw, &d)
		out = append(out, d)
	}
	return out
}
func (b *BoundTools) Execute(ctx context.Context, c gateway.ToolCall) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s, ok := b.specs[c.ToolName]
	if !ok {
		return nil, fmt.Errorf("tool not selected: %s", c.ToolName)
	}
	if err := validate(s.Definition.Schema, c.Arguments); err != nil {
		return nil, err
	}
	if s.Validate != nil {
		if err := s.Validate(c.Arguments); err != nil {
			return nil, err
		}
	}
	if s.Authorize != nil {
		if err := s.Authorize(ctx); err != nil {
			return nil, err
		}
	}
	return s.Handle(ctx, c.Arguments)
}

type Result struct {
	CallID    string
	ToolName  string
	Status    string
	ErrorCode string
	Content   any
	Metadata  map[string]any
}

func (b *BoundTools) Call(ctx context.Context, c gateway.ToolCall) Result {
	r := Result{CallID: c.CallID, ToolName: c.ToolName, Status: "success"}
	value, err := b.Execute(ctx, c)
	if err != nil {
		r.Status = "error"
		r.ErrorCode = "tool_rejected"
		r.Content = err.Error()
		return r
	}
	r.Content = value
	return r
}
