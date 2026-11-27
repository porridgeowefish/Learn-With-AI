package retrieval

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/modules/sources/internal/store"
)

type Excerpt struct {
	Text     string
	Citation Citation
}

type PromptText struct {
	Name    string `json:"name"`
	Text    string `json:"text"`
	Partial bool   `json:"partial,omitempty"`
}

// TextForPrompt shares selection and coverage between map generation and teaching.
// Citation metadata stays separate from the text sent to the model.
func (r *Reader) TextForPrompt(ctx context.Context, doc Document, budget int) (PromptText, []Citation, error) {
	excerpts, err := r.Excerpts(ctx, doc, budget)
	if err != nil {
		return PromptText{}, nil, err
	}
	if len(excerpts) == 0 {
		return PromptText{}, nil, store.ErrSourceNotReadable
	}
	parts := make([]string, 0, len(excerpts))
	citations := make([]Citation, 0, len(excerpts))
	bytes := 0
	for _, excerpt := range excerpts {
		parts = append(parts, excerpt.Text)
		citations = append(citations, excerpt.Citation)
		bytes += len(excerpt.Text)
	}
	complete := doc.ExtractionCoverage.CompleteWithinScope && bytes == doc.ExtractionCoverage.ReadableBytes
	return PromptText{Name: doc.Name, Text: strings.Join(parts, "\n\n［…］\n\n"), Partial: !complete}, citations, nil
}

// Excerpts loads a frozen document once. Long documents contribute passages
// from the beginning, middle and end while each citation covers actual text.
func (r *Reader) Excerpts(ctx context.Context, doc Document, budget int) ([]Excerpt, error) {
	_, _, raw, err := r.store.ReadDerivedRevision(doc.SourceID, doc.RevisionID, doc.DerivedVersionID, doc.ContentHash, doc.StructureHash)
	if err != nil {
		return nil, err
	}
	body := string(raw)
	nodes, _, _, err := scanOutline(ctx, body)
	if err != nil {
		return nil, err
	}
	ix := &index{Nodes: nodes}
	ranges := [][2]int{{0, len(body)}}
	if EstimateTokens(body) > budget {
		ranges = sampleRanges(body, budget)
	}
	out := make([]Excerpt, 0, len(ranges))
	for _, span := range ranges {
		start, end := span[0], span[1]
		if start >= end || strings.TrimSpace(body[start:end]) == "" {
			continue
		}
		citation, err := r.evidence(ctx, doc, ix, body, start, end)
		if err != nil {
			return nil, err
		}
		out = append(out, Excerpt{Text: body[start:end], Citation: citation})
	}
	return out, nil
}

func sampleRanges(body string, budget int) [][2]int {
	part := budget / 3
	firstEnd := nextRuneBoundary(body, len(body)/3)
	lastStart := nextRuneBoundary(body, 2*len(body)/3)
	middle := nextRuneBoundary(body, len(body)/2)
	midStart := middle - tokenSuffix(body[firstEnd:middle], part/2)
	tailStart := len(body) - tokenSuffix(body[lastStart:], budget-2*part)
	ranges := [][2]int{
		{0, tokenPrefix(body[:firstEnd], part)},
		{midStart, midStart + tokenPrefix(body[midStart:lastStart], part)},
		{tailStart, len(body)},
	}
	for _, block := range protectedBlocks(body) {
		for i := range ranges {
			start, end := ranges[i][0], ranges[i][1]
			if block.Start < start && start < block.End {
				ranges[i][0] = block.End
			}
			if block.Start < end && end < block.End {
				ranges[i][1] = block.Start
			}
		}
	}
	return ranges
}

func nextRuneBoundary(body string, offset int) int {
	for offset < len(body) && !utf8.RuneStart(body[offset]) {
		offset++
	}
	return offset
}

// tokenSuffix mirrors tokenPrefix for the end of a passage without reversing
// the string or cutting through a UTF-8 rune.
func tokenSuffix(body string, budget int) int {
	cost, ascii, end := 0, 0, len(body)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(body[:end])
		if r < utf8.RuneSelf {
			if ascii%3 == 0 {
				cost++
			}
			ascii++
		} else {
			cost++
			ascii = 0
		}
		if cost > budget {
			break
		}
		end -= size
	}
	return len(body) - end
}
