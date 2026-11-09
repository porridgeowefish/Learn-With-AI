package teacherservice

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/visuals"
)

var searchFigures = visuals.Search
var searchManufacturing = visuals.SearchManufacturing

var visualTool = ai.Tool{Name: "find_learning_visual", Description: promptText("tools/visual/description"), Schema: map[string]any{
	"type": "object", "additionalProperties": false,
	"properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"diagram", "molecule", "manufacturing"}}, "query": map[string]any{"type": "string", "maxLength": 120}},
	"required":   []string{"kind", "query"},
}}

var visualInstructions = promptText("tools/visual/instructions")

func executeVisualTool(ctx context.Context, args map[string]any, room int, emit func(StreamFrame)) (string, string) {
	kind, _ := args["kind"].(string)
	query, _ := args["query"].(string)
	if len(args) != 2 || (kind != "diagram" && kind != "molecule" && kind != "manufacturing") || strings.TrimSpace(query) == "" || len([]rune(query)) > 120 {
		return "", "invalid_arguments"
	}
	emit(StreamFrame{Type: "search-started", Data: map[string]any{"query": query, "source": "open-figures"}})
	if kind == "manufacturing" {
		return executeManufacturingVisual(ctx, query, room, emit)
	}
	figures, err := searchFigures(ctx, kind, query)
	if err != nil {
		emit(StreamFrame{Type: "search-failed", Data: map[string]any{"query": query, "source": "open-figures", "code": "visuals_unavailable"}})
		return "", "visuals_unavailable"
	}
	if figures == nil {
		figures = []visuals.Figure{}
	}
	for {
		raw, _ := json.Marshal(map[string]any{"figures": figures})
		if ai.EstimateTextTokens(string(raw)) <= min(2200, room) {
			emit(StreamFrame{Type: "search-completed", Data: map[string]any{"query": query, "source": "open-figures", "count": len(figures)}})
			return string(raw), ""
		}
		if len(figures) == 0 {
			return "", "budget_exhausted"
		}
		figures = figures[:len(figures)-1]
	}
}

func executeManufacturingVisual(ctx context.Context, query string, room int, emit func(StreamFrame)) (string, string) {
	result, err := searchManufacturing(ctx, query)
	if err != nil {
		emit(StreamFrame{Type: "search-failed", Data: map[string]any{"query": query, "source": "open-figures", "code": "visuals_unavailable"}})
		return "", "visuals_unavailable"
	}
	// Every slice belongs to this invocation, including when the search seam or
	// a future provider returns cached groups. Budget reduction must be local.
	result.Groups = append([]visuals.FigureGroup(nil), result.Groups...)
	result.Gaps = append([]visuals.FigureGap(nil), result.Gaps...)
	for i := range result.Groups {
		result.Groups[i].Figures = append([]visuals.Figure(nil), result.Groups[i].Figures...)
	}
	trimmed := false
	for {
		raw, _ := json.Marshal(result)
		if ai.EstimateTextTokens(string(raw)) <= min(4000, room) {
			count := 0
			for _, group := range result.Groups {
				count += len(group.Figures)
			}
			if count == 0 {
				emit(StreamFrame{Type: "search-failed", Data: map[string]any{"query": query, "source": "open-figures", "code": "no_verified_figures"}})
			} else {
				emit(StreamFrame{Type: "search-completed", Data: map[string]any{"query": query, "source": "open-figures", "count": count, "partial": len(result.Gaps) > 0}})
			}
			return string(raw), ""
		}
		if !trimmed {
			result.Gaps = append(result.Gaps, visuals.FigureGap{Stage: "budget", Reason: "部分图示或导读因本轮上下文预算省略。"})
			trimmed = true
		}
		removed := false
		for i := len(result.Groups) - 1; i >= 0; i-- {
			if n := len(result.Groups[i].Figures); n > 0 {
				result.Groups[i].Figures = result.Groups[i].Figures[:n-1]
				removed = true
				break
			}
		}
		if removed {
			continue
		}
		if len(result.Groups) > 0 {
			result.Groups = result.Groups[:len(result.Groups)-1]
			continue
		}
		emit(StreamFrame{Type: "search-failed", Data: map[string]any{"query": query, "source": "open-figures", "code": "budget_exhausted"}})
		return "", "budget_exhausted"
	}
}
