package sources

import "github.com/xmz14/lll/backend-go/internal/modules/ai"

func Tools() []ai.Tool {
	prop := func(kind string) map[string]any { return map[string]any{"type": kind} }
	schema := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": append([]string{}, required...)}
	}
	id := prop("string")
	limit := map[string]any{"type": "integer", "minimum": 1, "maximum": 20}
	return []ai.Tool{
		{Name: "list_source_documents", Description: sourceToolDescription("tools/source-list-source-documents"), Schema: schema(map[string]any{"cursor": id, "limit": limit})},
		{Name: "get_source_outline", Description: sourceToolDescription("tools/source-get-source-outline"), Schema: schema(map[string]any{"sourceId": id, "revisionId": id, "cursor": id, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}, "target": schema(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"page", "slide", "lines", "section", "sheet"}}, "start": map[string]any{"type": "integer", "minimum": 1}, "end": map[string]any{"type": "integer", "minimum": 1}, "name": map[string]any{"type": "string", "maxLength": 256}}, "kind")}, "sourceId", "revisionId")},
		{Name: "search_source", Description: sourceToolDescription("tools/source-search-source"), Schema: schema(map[string]any{"queries": map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 256}}, "sourceIds": map[string]any{"type": "array", "maxItems": 20, "items": id}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 6}}, "queries")},
		{Name: "read_source", Description: sourceToolDescription("tools/source-read-source"), Schema: schema(map[string]any{"sourceId": id, "revisionId": id, "locator": id, "cursor": id, "maxTokens": map[string]any{"type": "integer", "minimum": 0, "maximum": 4000}}, "sourceId", "revisionId")},
	}
}

func sourceToolDescription(ref string) string {
	text, err := ai.RenderPrompt(ref, nil)
	if err != nil {
		return ref
	}
	return text
}

// CheckedTools fails preparation before exposing definitions with missing instructions.
func CheckedTools() ([]ai.Tool, error) {
	for _, ref := range []string{"tools/source-list-source-documents", "tools/source-get-source-outline", "tools/source-search-source", "tools/source-read-source"} {
		if _, err := ai.RenderPrompt(ref, nil); err != nil {
			return nil, err
		}
	}
	return Tools(), nil
}
