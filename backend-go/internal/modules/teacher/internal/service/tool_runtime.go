package teacherservice

import (
	"context"
	"encoding/json"
	"errors"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/learningprofile"
	sources "github.com/xmz14/lll/backend-go/internal/modules/sources"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/websearch"
	"strings"
)

type toolOutcome struct {
	Text, Code string
	Final      bool
}
type responseTools struct {
	bound                                                        *ai.BoundTools
	reading                                                      *responseSources
	searcher                                                     websearch.Searcher
	service                                                      *Service
	slug, responseID                                             string
	callID                                                       string
	emit                                                         func(StreamFrame)
	room, sourceCalls, webCalls, visualCalls, manufacturingCalls int
}

func bindResponseTools(definitions []ai.Tool, r *responseSources, searcher websearch.Searcher, s *Service, slug, responseID string, emit func(StreamFrame)) (*responseTools, error) {
	runtime := &responseTools{reading: r, searcher: searcher, service: s, slug: slug, responseID: responseID, emit: emit}
	handles := map[string]ai.ToolHandle{"search_web": runtime.web, "find_learning_visual": runtime.visual}
	for _, name := range []string{"list_source_documents", "get_source_outline", "search_source", "read_source"} {
		name := name
		handles[name] = func(ctx context.Context, args map[string]any) (any, error) { return runtime.source(ctx, name, args) }
	}
	var registry ai.ToolRegistry
	names := []string{}
	for _, definition := range definitions {
		handle, ok := handles[definition.Name]
		if !ok {
			return nil, errors.New("teacher tool has no registered implementation: " + definition.Name)
		}
		if e := registry.Register(ai.ToolSpec{Definition: definition, Handle: handle}); e != nil {
			return nil, e
		}
		names = append(names, definition.Name)
	}
	var e error
	runtime.bound, e = registry.Bind(names)
	return runtime, e
}
func (r *responseTools) execute(ctx context.Context, call ai.ToolCall, room int) toolOutcome {
	r.room = room
	r.callID = call.CallID
	value, e := r.bound.Execute(ctx, call)
	if e != nil {
		return toolOutcome{Code: "invalid_arguments"}
	}
	return value.(toolOutcome)
}
func (r *responseTools) source(ctx context.Context, name string, args map[string]any) (any, error) {
	r.sourceCalls++
	allocation := min(4000, max(0, r.room), r.reading.limit-r.reading.used)
	if r.sourceCalls > 12 {
		return toolOutcome{Code: "tool_limit_reached", Final: true}, nil
	}
	if allocation < 128 {
		return toolOutcome{Code: "budget_exhausted", Final: true}, nil
	}
	operation := strings.TrimSuffix(strings.TrimPrefix(name, "get_source_"), "_source")
	if name == "search_source" {
		operation = "search"
	}
	if name == "read_source" {
		operation = "read"
	}
	sourceID, _ := args["sourceId"].(string)
	revisionID, _ := args["revisionId"].(string)
	progress := map[string]any{"projectSlug": r.slug, "responseId": r.responseID, "operationId": r.callID, "phase": "started", "operation": operation, "sourceId": sourceID, "revisionId": revisionID}
	r.service.sourceReading(progress, r.emit)
	result := r.reading.execute(ctx, name, args, allocation)
	encoded, _ := json.Marshal(result)
	done := map[string]any{"projectSlug": r.slug, "responseId": r.responseID, "operationId": r.callID, "phase": "completed", "operation": operation, "sourceId": sourceID, "revisionId": revisionID}
	if data, ok := result.Data.(sources.SourceReadData); ok {
		done["displayRange"] = data.Citation.DisplayRange
	}
	out := toolOutcome{Text: string(encoded)}
	if result.Error != nil {
		done["phase"] = "failed"
		done["reason"] = result.Error.Code
		out.Code = result.Error.Code
		out.Final = out.Code == "budget_exhausted"
	}
	r.service.sourceReading(done, r.emit)
	return out, nil
}
func (r *responseTools) visual(ctx context.Context, args map[string]any) (any, error) {
	r.visualCalls++
	if args["kind"] == "manufacturing" {
		r.manufacturingCalls++
	}
	if r.visualCalls > 2 || r.manufacturingCalls > 1 {
		return toolOutcome{Code: "tool_limit_reached", Final: true}, nil
	}
	if r.room < 256 {
		return toolOutcome{Code: "budget_exhausted", Final: true}, nil
	}
	text, code := executeVisualTool(ctx, args, r.room, r.emit)
	return toolOutcome{Text: text, Code: code}, nil
}
func (r *responseTools) web(ctx context.Context, args map[string]any) (any, error) {
	r.webCalls++
	live, e := learningprofile.Read()
	if r.searcher == nil || e != nil || !live.Settings.Search.Enabled {
		return toolOutcome{Code: "websearch_disabled"}, nil
	}
	if r.webCalls > 2 {
		return toolOutcome{Code: "tool_limit_reached", Final: true}, nil
	}
	if r.room < 256 {
		return toolOutcome{Code: "budget_exhausted", Final: true}, nil
	}
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return toolOutcome{Code: "invalid_arguments"}, nil
	}
	r.emit(StreamFrame{Type: "search-started", Data: map[string]any{"query": query}})
	results, e := r.searcher.Search(ctx, query)
	if e != nil {
		r.emit(StreamFrame{Type: "search-failed", Data: map[string]any{"query": query, "code": "websearch_unavailable"}})
		return toolOutcome{Code: "websearch_unavailable"}, nil
	}
	r.emit(StreamFrame{Type: "search-completed", Data: map[string]any{"query": query, "count": len(results)}})
	return toolOutcome{Text: ai.TruncateTokens(websearch.FormatResults(results), min(2000, r.room))}, nil
}
