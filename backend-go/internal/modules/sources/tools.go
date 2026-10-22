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
		{Name: "list_source_documents", Description: "分页查看当前资料目录；目录不代表已读正文。", Schema: schema(map[string]any{"cursor": id, "limit": limit})},
		{Name: "get_source_outline", Description: "定位已知资料的章节、页或行范围。", Schema: schema(map[string]any{"sourceId": id, "revisionId": id, "cursor": id, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}, "target": schema(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"page", "slide", "lines", "section", "sheet"}}, "start": map[string]any{"type": "integer", "minimum": 1}, "end": map[string]any{"type": "integer", "minimum": 1}, "name": map[string]any{"type": "string", "maxLength": 256}}, "kind")}, "sourceId", "revisionId")},
		{Name: "search_source", Description: "用1至3个关键词查询当前资料，只返回短原文预览。", Schema: schema(map[string]any{"queries": map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 256}}, "sourceIds": map[string]any{"type": "array", "maxItems": 20, "items": id}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 6}}, "queries")},
		{Name: "read_source", Description: "通过服务发行的locator或cursor连续读取原文，并获得实际证据和下一范围。", Schema: schema(map[string]any{"sourceId": id, "revisionId": id, "locator": id, "cursor": id, "maxTokens": map[string]any{"type": "integer", "minimum": 1, "maximum": 4000}}, "sourceId", "revisionId")},
	}
}
