package teacherservice

import (
	"context"
	"encoding/json"
	"fmt"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	sources "github.com/xmz14/lll/backend-go/internal/modules/sources"
	conversation "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"

	"regexp"
	"sort"
	"strings"
)

var sourceToolInstructions = promptText("tools/source-instructions")

func isSourceTool(name string) bool {
	switch name {
	case "list_source_documents", "get_source_outline", "search_source", "read_source":
		return true
	}
	return false
}

type responseSources struct {
	allowed     map[string]bool
	reader      *sources.Reader
	snapshot    []sources.SourceDocument
	citations   map[string]sources.SourceCitation
	coverage    map[string]*conversation.ReadingCoverage
	used, limit int
}

func newResponseSources(ctx context.Context, slug string, limit int) *responseSources {
	r := &responseSources{citations: map[string]sources.SourceCitation{}, coverage: map[string]*conversation.ReadingCoverage{}, limit: limit}
	if st, err := sources.New(slug); err == nil {
		r.reader = sources.NewReader(st)
		r.snapshot, _ = r.reader.Snapshot(ctx)
	}
	return r
}
func sourceError(code string) sources.SourceResult {
	return sources.SourceResult{Status: "error", Error: &sources.SourceToolError{Code: code, Message: "资料操作未完成，请依据已返回证据继续。"}, Truncated: code == "budget_exhausted" || code == "tool_limit_reached", TruncationReason: code}
}
func (r *responseSources) execute(ctx context.Context, name string, args map[string]any, budget int) sources.SourceResult {
	if r.allowed != nil && !r.allowed[name] {
		return sourceError("tool_not_offered")
	}
	if r.reader == nil {
		return sourceError("source_not_readable")
	}
	budget = min(budget, r.limit-r.used)
	if budget < 128 {
		return sourceError("budget_exhausted")
	}
	result := sources.ExecuteTool(ctx, r.reader, r.snapshot, ai.ToolCall{ToolName: name, Arguments: args}, sources.ToolPolicy{TeacherDefaults: true, Budget: budget, ReadDefault: 2000})
	var remember []sources.SourceCitation
	var next *sources.SourceRange
	complete := false
	switch data := result.Data.(type) {
	case sources.SourceSearchData:
		kept := data.Hits[:0]
		for _, hit := range data.Hits {
			if _, seen := r.seenRange(hit.Citation); !seen {
				kept = append(kept, hit)
				remember = append(remember, hit.Citation)
			}
		}
		data.Hits = kept
		result.Data = data
	case sources.SourceReadData:
		if prior, seen := r.seenRange(data.Citation); seen {
			result.Data = map[string]any{"alreadyRead": true, "evidenceId": prior.EvidenceID, "next": data.Next}
			break
		}
		if data.Next != "" && len(data.Ranges) > 0 {
			end := data.Ranges[len(data.Ranges)-1].EndByte
			next = &sources.SourceRange{StartByte: end, EndByte: data.ScopeRange.EndByte}
		}
		remember = append(remember, data.Citation)
		complete = data.Coverage.CompleteWithinScope
	}
	raw, _ := json.Marshal(result)
	if ai.EstimateTextTokens(string(raw)) > budget {
		return sourceError("budget_exhausted")
	}
	r.used += ai.EstimateTextTokens(string(raw))
	for _, citation := range remember {
		r.remember(citation, next, complete)
	}
	if data, ok := result.Data.(sources.SourceReadData); ok {
		if coverage := r.coverage[data.Citation.SourceID+":"+data.Citation.RevisionID]; coverage != nil {
			coverage.Scope = fmt.Sprintf("可读原文第%d–%d行", data.ScopeRange.StartLine, data.ScopeRange.EndLine)
			coverage.CompleteWithinScope = false
			for _, rng := range coverage.CoveredRanges {
				if rng.StartByte <= data.ScopeRange.StartByte && rng.EndByte >= data.ScopeRange.EndByte {
					coverage.CompleteWithinScope = true
				}
			}
		}
	}
	return result
}
func (r *responseSources) seenRange(c sources.SourceCitation) (sources.SourceCitation, bool) {
	for _, prior := range r.citations {
		if c.SourceID == prior.SourceID && c.RevisionID == prior.RevisionID && c.ContentHash == prior.ContentHash && c.StartByte == prior.StartByte && c.EndByte == prior.EndByte {
			return prior, true
		}
	}
	return sources.SourceCitation{}, false
}
func (r *responseSources) remember(c sources.SourceCitation, next *sources.SourceRange, complete bool) {
	if c.EvidenceID == "" {
		return
	}
	r.citations[c.EvidenceID] = c
	key := c.SourceID + ":" + c.RevisionID
	coverage := r.coverage[key]
	if coverage == nil {
		coverage = &conversation.ReadingCoverage{SourceID: c.SourceID, RevisionID: c.RevisionID, ContentHash: c.ContentHash, Scope: "已返回原文范围"}
		r.coverage[key] = coverage
	}
	coverage.CoveredRanges = append(coverage.CoveredRanges, sources.SourceRange{StartByte: c.StartByte, EndByte: c.EndByte, StartLine: c.StartLine, EndLine: c.EndLine, UnitRanges: append([]sources.SourceUnitRange(nil), c.UnitRanges...)})
	if next != nil || complete {
		coverage.NextRange = next
	}
	sort.Slice(coverage.CoveredRanges, func(i, j int) bool { return coverage.CoveredRanges[i].StartByte < coverage.CoveredRanges[j].StartByte })
	merged := coverage.CoveredRanges[:0]
	for _, rng := range coverage.CoveredRanges {
		if len(merged) > 0 && rng.StartByte <= merged[len(merged)-1].EndByte {
			last := &merged[len(merged)-1]
			last.EndByte = max(last.EndByte, rng.EndByte)
			last.EndLine = max(last.EndLine, rng.EndLine)
			seen := make(map[sources.SourceUnitRange]bool, len(last.UnitRanges))
			for _, unit := range last.UnitRanges {
				seen[unit] = true
			}
			for _, unit := range rng.UnitRanges {
				if !seen[unit] {
					last.UnitRanges = append(last.UnitRanges, unit)
					seen[unit] = true
				}
			}
		} else {
			merged = append(merged, rng)
		}
	}
	coverage.CoveredRanges = merged
	coverage.CompleteWithinScope = complete && coverage.NextRange == nil
}
func (r *responseSources) bootstrap(ctx context.Context, question string, budget int) string {
	if budget < 256 {
		return ""
	}
	list := r.execute(ctx, "list_source_documents", map[string]any{"limit": 10}, min(1000, budget/2))
	raw, _ := json.Marshal(list)
	appendix := "\n\n【本轮可查阅资料目录】\n" + string(raw)
	remaining := min(1500, budget-ai.EstimateTextTokens(appendix)-128)
	if remaining > 256 && strings.TrimSpace(question) != "" {
		result := r.execute(ctx, "search_source", map[string]any{"queries": []string{truncateUTF8(question, 256)}, "limit": 2}, remaining)
		if data, ok := result.Data.(sources.SourceSearchData); ok && len(data.Hits) > 0 {
			raw, _ = json.Marshal(result)
			appendix += "\n【相关短片段；只证明实际预览】\n" + string(raw)
		}
	}
	return appendix
}
func (r *responseSources) continuation(ctx context.Context, messages []conversation.Message) string {
	if r.reader == nil {
		return ""
	}
	for i, scanned := len(messages)-1, 0; i >= 0 && scanned < 8; i-- {
		if messages[i].Role != "teacher" || messages[i].Status != "completed" {
			continue
		}
		scanned++
		for _, c := range messages[i].ReadingCoverage {
			if c.NextRange == nil {
				continue
			}
			cursor, err := r.reader.Continuation(ctx, r.snapshot, c.SourceID, c.RevisionID, c.ContentHash, c.NextRange.StartByte, c.NextRange.EndByte)
			if err != nil {
				continue
			}
			copyCoverage := c
			copyCoverage.CoveredRanges = append([]sources.SourceRange(nil), c.CoveredRanges...)
			r.coverage[c.SourceID+":"+c.RevisionID] = &copyCoverage
			raw, _ := json.Marshal(map[string]any{"sourceId": c.SourceID, "revisionId": c.RevisionID, "cursor": cursor, "coveredRanges": c.CoveredRanges, "scope": c.Scope})
			return "\n【校验后的继续阅读位置；旧回答不是原文证据】\n" + string(raw)
		}
		if len(messages[i].ReadingCoverage) > 0 {
			break
		}
	}
	return ""
}

var sourceCitationMarker = regexp.MustCompile(`\[\[source:([^\]\s]+)\]\]`)

func (r *responseSources) verifiedAnswer(ctx context.Context, text string) (string, []sources.SourceCitation, []conversation.ReadingCoverage) {
	var citations []sources.SourceCitation
	used := map[string]bool{}
	text = sourceCitationMarker.ReplaceAllStringFunc(text, func(marker string) string {
		id := sourceCitationMarker.FindStringSubmatch(marker)[1]
		c, ok := r.citations[id]
		if !ok || r.reader == nil || r.reader.ValidateCitation(ctx, c) != nil {
			return "[来源未核实]"
		}
		if !used[id] {
			citations = append(citations, c)
			used[id] = true
		}
		return marker
	})
	var coverage []conversation.ReadingCoverage
	for _, c := range r.coverage {
		valid := false
		for _, e := range r.citations {
			if e.SourceID == c.SourceID && r.reader.ValidateCitation(ctx, e) == nil {
				valid = true
				break
			}
		}
		if valid {
			coverage = append(coverage, *c)
		}
	}
	return text, citations, coverage
}
