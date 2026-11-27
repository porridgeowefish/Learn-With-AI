package teacherservice

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/sources"
)

func (r *responseSources) attachmentText(ctx context.Context, budget int) string {
	const header = "\n\n【本轮附件原文】partial 表示节选或提取不完整，［…］标出节选间隔。需要其他段落时可查阅资料。\n"
	docs := make([]sources.SourceDocument, 0, min(32, len(r.snapshot)))
	for _, doc := range r.snapshot {
		if doc.Readable {
			docs = append(docs, doc)
			if len(docs) == 32 {
				break
			}
		}
	}
	if len(docs) == 0 {
		return ""
	}
	remaining := budget - ai.EstimateTextTokens(header+"[]")
	rows := make([]string, 0, len(docs))
	for i, doc := range docs {
		// Budget the serialized row as well as its plain-text body.
		overhead := ai.EstimateTextTokens(doc.Name) + 20
		allowance := min(4000, max(0, (remaining/(len(docs)-i)-overhead))/2)
		if allowance < 3 {
			continue
		}
		text, _, err := r.reader.TextForPrompt(ctx, doc, allowance)
		if err != nil {
			continue
		}
		raw, _ := json.Marshal(text)
		cost := ai.EstimateTextTokens(string(raw)) + 1
		if cost > remaining {
			continue
		}
		rows = append(rows, string(raw))
		remaining -= cost
	}
	if len(rows) == 0 {
		return ""
	}
	appendix := header + "[" + strings.Join(rows, ",") + "]"
	r.used += ai.EstimateTextTokens(appendix)
	return appendix
}

func truncateUTF8(text string, bytes int) string {
	if bytes <= 0 {
		return ""
	}
	if len(text) <= bytes {
		return text
	}
	for bytes > 0 && !utf8.RuneStart(text[bytes]) {
		bytes--
	}
	return text[:bytes]
}
