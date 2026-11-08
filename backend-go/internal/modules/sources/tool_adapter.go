package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"strings"
)

type ToolPolicy struct {
	TeacherDefaults     bool
	Budget, ReadDefault int
}

func sourceError(code string) SourceResult {
	return SourceResult{Status: "error", Error: &SourceToolError{Code: code, Message: "资料操作未完成"}}
}
func strictArguments(args map[string]any, out any) error {
	raw, e := json.Marshal(args)
	if e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func listSourceDocuments(ctx context.Context, reader *Reader, docs []SourceDocument, args map[string]any, policy ToolPolicy) SourceResult {
	budget := policy.Budget
	var result SourceResult
	var a ListSourceArgs
	if strictArguments(args, &a) != nil {
		return sourceError("invalid_arguments")
	}
	a.MaxTokens = max(1, budget/2)
	result = reader.List(ctx, docs, a)
	return result
}
func getSourceOutline(ctx context.Context, reader *Reader, docs []SourceDocument, args map[string]any, policy ToolPolicy) SourceResult {
	budget := policy.Budget
	var result SourceResult
	var a OutlineSourceArgs
	if strictArguments(args, &a) != nil {
		return sourceError("invalid_arguments")
	}
	a.MaxTokens = max(1, budget/2)
	result = reader.Outline(ctx, docs, a)
	return result
}
func searchSource(ctx context.Context, reader *Reader, docs []SourceDocument, args map[string]any, policy ToolPolicy) SourceResult {
	budget := policy.Budget
	var result SourceResult
	var a SearchSourceArgs
	if strictArguments(args, &a) != nil {
		return sourceError("invalid_arguments")
	}
	a.MaxTokens = max(1, (budget-700)/2)
	if policy.TeacherDefaults && a.Limit <= 0 {
		a.Limit = 4
	}
	if policy.TeacherDefaults {
		a.Limit = min(a.Limit, max(1, budget/700))
	}
	result = reader.Search(ctx, docs, a)
	return result
}
func readSource(ctx context.Context, reader *Reader, docs []SourceDocument, args map[string]any, policy ToolPolicy) SourceResult {
	var result SourceResult
	var a ReadSourceArgs
	if strictArguments(args, &a) != nil {
		return sourceError("invalid_arguments")
	}
	if a.MaxTokens < 0 || a.MaxTokens > 4000 {
		return sourceError("invalid_arguments")
	}
	a.MaxTokens = readTokenLimit(a.MaxTokens, policy)
	result = reader.Read(ctx, docs, a)
	return result
}
func BindTools(reader *Reader, docs []SourceDocument, policy ToolPolicy) (*ai.BoundTools, error) {
	var registry ai.ToolRegistry
	names := []string{}
	handles := map[string]func(context.Context, *Reader, []SourceDocument, map[string]any, ToolPolicy) SourceResult{"list_source_documents": listSourceDocuments, "get_source_outline": getSourceOutline, "search_source": searchSource, "read_source": readSource}
	definitions, err := CheckedTools()
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		name := definition.Name
		err := registry.Register(ai.ToolSpec{Definition: definition, DescriptionRef: "tools/source-" + strings.ReplaceAll(name, "_", "-"), SideEffects: "read", Authorize: func(ctx context.Context) error {
			if reader == nil {
				return fmt.Errorf("source reader unavailable")
			}
			return ctx.Err()
		}, Handle: func(ctx context.Context, args map[string]any) (any, error) {
			return handles[name](ctx, reader, docs, args, policy), nil
		}})
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return registry.Bind(names)
}
func ExecuteTool(ctx context.Context, reader *Reader, docs []SourceDocument, call ai.ToolCall, policy ToolPolicy) SourceResult {
	bound, err := BindTools(reader, docs, policy)
	if err != nil {
		return sourceError("invalid_tool")
	}
	value, err := bound.Execute(ctx, call)
	if err != nil {
		return sourceError("invalid_arguments")
	}
	return value.(SourceResult)
}
func readTokenLimit(requested int, policy ToolPolicy) int {
	if requested <= 0 {
		requested = policy.ReadDefault
	}
	return min(max(1, requested), max(1, (policy.Budget-800)/2), 4000)
}
