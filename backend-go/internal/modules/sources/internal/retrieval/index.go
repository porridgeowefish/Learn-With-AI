package retrieval

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const indexerVersion = "structured-2"

type indexNode struct {
	ID     string     `json:"nodeId"`
	Parent string     `json:"parentId,omitempty"`
	Title  string     `json:"title"`
	Level  int        `json:"level"`
	Start  int        `json:"startByte"`
	End    int        `json:"endByte"`
	Unit   *UnitRange `json:"unit,omitempty"`
}
type chunk struct {
	Start  int      `json:"startByte"`
	End    int      `json:"endByte"`
	Path   []string `json:"headingPath"`
	Length int      `json:"length"`
}
type index struct {
	DerivedVersionID    string                 `json:"derivedVersionId,omitempty"`
	StructureHash       string                 `json:"structureHash,omitempty"`
	SchemaVersion       int                    `json:"schemaVersion"`
	SourceID            string                 `json:"sourceId"`
	RevisionID          string                 `json:"revisionId"`
	ContentHash         string                 `json:"contentSHA256"`
	IndexerVersion      string                 `json:"indexerVersion"`
	BuiltAt             time.Time              `json:"builtAt"`
	ExtractionTruncated bool                   `json:"extractionTruncated"`
	ReadableBytes       int                    `json:"readableBytes"`
	Partial             bool                   `json:"partial"`
	Nodes               []indexNode            `json:"outline"`
	Chunks              []chunk                `json:"chunks"`
	Postings            map[string]map[int]int `json:"lexical"`
	AvgLength           float64                `json:"averageLength"`
}

var headingPattern = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)\s*$`)
var pagePattern = regexp.MustCompile(`^第\s*(\d+)\s*页$`)
var slidePattern = regexp.MustCompile(`^第\s*(\d+)\s*张幻灯片$`)

// EstimateTokens deliberately errs toward larger CJK/math costs and is not a model tokenizer.
func EstimateTokens(text string) int {
	count := 0
	ascii := 0
	for _, r := range text {
		if r < 128 {
			ascii++
		} else {
			count++
		}
	}
	return count + (ascii+2)/3
}
func tokenPrefix(text string, budget int) int {
	if budget <= 0 {
		return 0
	}
	cost := 0
	ascii := 0
	end := 0
	for i, r := range text {
		if r < 128 {
			ascii++
			if ascii%3 == 1 {
				cost++
			}
		} else {
			cost++
			ascii = 0
		}
		if cost > budget {
			return i
		}
		end = i + utf8.RuneLen(r)
	}
	return end
}
func terms(text string) []string {
	var out []string
	var latin, cjk []rune
	flushLatin := func() {
		if len(latin) > 0 && len(latin) <= 128 {
			out = append(out, strings.ToLower(string(latin)))
		}
		latin = nil
	}
	flushCJK := func() {
		if len(cjk) == 1 {
			out = append(out, string(cjk))
		}
		for i := 0; i+1 < len(cjk); i++ {
			out = append(out, string(cjk[i:i+2]))
		}
		cjk = nil
	}
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
			flushCJK()
			latin = append(latin, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return out
}
func scanOutline(ctx context.Context, body string) ([]indexNode, []int, bool, error) {
	var nodes []indexNode
	partial := false
	offset := 0
	fence := ""
	var stack []int
	var boundaries = []int{0}
	for _, line := range strings.SplitAfter(body, "\n") {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			marker := trim[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
		}
		if fence == "" {
			if len(nodes) >= 8000 && headingPattern.MatchString(strings.TrimRight(line, "\r\n")) {
				partial = true
			}
			if match := headingPattern.FindStringSubmatch(strings.TrimRight(line, "\r\n")); match != nil && len(nodes) < 8000 {
				level := len(match[1])
				title := match[2]
				if utf8.RuneCountInString(title) > 256 {
					title = string([]rune(title)[:256])
					partial = true
				}
				for len(stack) > 0 && nodes[stack[len(stack)-1]].Level >= level {
					nodes[stack[len(stack)-1]].End = offset
					stack = stack[:len(stack)-1]
				}
				parent := ""
				if len(stack) > 0 {
					parent = nodes[stack[len(stack)-1]].ID
				}
				node := indexNode{ID: fmt.Sprintf("node_%d", len(nodes)+1), Parent: parent, Title: title, Level: level, Start: offset, End: len(body)}
				if m := pagePattern.FindStringSubmatch(title); m != nil {
					n, _ := strconv.Atoi(m[1])
					node.Unit = &UnitRange{Kind: "page", Start: n, End: n}
				}
				if m := slidePattern.FindStringSubmatch(title); m != nil {
					n, _ := strconv.Atoi(m[1])
					node.Unit = &UnitRange{Kind: "slide", Start: n, End: n}
				}
				if strings.HasPrefix(title, "工作表：") {
					node.Unit = &UnitRange{Kind: "sheet", Name: strings.TrimPrefix(title, "工作表：")}
				}
				nodes = append(nodes, node)
				stack = append(stack, len(nodes)-1)
				if offset > 0 {
					boundaries = append(boundaries, offset)
				}
			}
		}
		offset += len(line)
		if trim == "" && offset < len(body) && len(boundaries) < 30000 {
			boundaries = append(boundaries, offset)
		}
	}
	boundaries = append(boundaries, len(body))
	return nodes, boundaries, partial, nil
}

func buildIndex(ctx context.Context, d Document, body string) (*index, error) {
	nodes, boundaries, partial, err := scanOutline(ctx, body)
	if err != nil {
		return nil, err
	}
	ix := &index{SchemaVersion: 1, SourceID: d.SourceID, RevisionID: d.RevisionID, ContentHash: d.ContentHash, IndexerVersion: indexerVersion, BuiltAt: time.Now().UTC(), ExtractionTruncated: d.ExtractionCoverage.ExtractionTruncated, ReadableBytes: len(body), Postings: map[string]map[int]int{}, Nodes: nodes, Partial: partial}
	ix.DerivedVersionID = d.DerivedVersionID
	ix.StructureHash = d.StructureHash
	sort.Ints(boundaries)
	start := 0
	protected := protectedBlocks(body)
	add := func(end int) {
		for _, b := range protected {
			if end > b.Start && end < b.End {
				end = b.End
			}
		}
		if end <= start {
			return
		}
		var path []string
		for _, n := range ix.Nodes {
			if n.Start <= start && start < n.End {
				path = append(path, n.Title)
			}
		}
		ix.Chunks = append(ix.Chunks, chunk{Start: start, End: end, Path: path})
		start = end
	}
	for _, boundary := range boundaries {
		for start < boundary {
			end := start + tokenPrefix(body[start:boundary], 700)
			if end <= start || end == boundary {
				break
			}
			add(end)
		}
		if boundary > start && (EstimateTokens(body[start:boundary]) >= 450 || boundary == len(body) || isHeading(ix, boundary)) {
			add(boundary)
		}
	}
	postings := 0
	total := 0
	for i := range ix.Chunks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tokens := terms(body[ix.Chunks[i].Start:ix.Chunks[i].End])
		ix.Chunks[i].Length = len(tokens)
		total += len(tokens)
		for _, term := range tokens {
			p, ok := ix.Postings[term]
			if !ok {
				if len(ix.Postings) >= 100000 {
					ix.Partial = true
					continue
				}
				p = map[int]int{}
				ix.Postings[term] = p
			}
			if _, ok := p[i]; !ok {
				if postings >= 800000 {
					ix.Partial = true
					continue
				}
				postings++
			}
			p[i]++
		}
	}
	if len(ix.Chunks) > 0 {
		ix.AvgLength = float64(total) / float64(len(ix.Chunks))
	}
	return ix, nil
}
func isHeading(ix *index, pos int) bool {
	for _, n := range ix.Nodes {
		if n.Start == pos {
			return true
		}
	}
	return false
}

type scoredChunk struct {
	Index int
	Score float64
}

func rank(ix *index, query []string, name string) []scoredChunk {
	unique := map[string]bool{}
	for _, q := range query {
		for _, term := range terms(q) {
			unique[term] = true
		}
	}
	scores := map[int]float64{}
	avg := math.Max(ix.AvgLength, 1)
	for term := range unique {
		p := ix.Postings[term]
		idf := math.Log(1 + (float64(len(ix.Chunks)-len(p))+0.5)/(float64(len(p))+0.5))
		for i, frequency := range p {
			f := float64(frequency)
			length := float64(ix.Chunks[i].Length)
			scores[i] += idf * f * 2.2 / (f + 1.2*(0.25+0.75*length/avg))
		}
		for i, c := range ix.Chunks {
			navigation := strings.ToLower(strings.Join(c.Path, " ") + " " + name)
			if strings.Contains(navigation, term) {
				scores[i] += 0.2
			}
		}
	}
	out := make([]scoredChunk, 0, len(scores))
	for i, score := range scores {
		if score > 0 {
			out = append(out, scoredChunk{i, score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return ix.Chunks[out[i].Index].Start < ix.Chunks[out[j].Index].Start
		}
		return out[i].Score > out[j].Score
	})
	return out
}
func rangeFor(ix *index, body string, start, end int) Range {
	r := Range{StartByte: start, EndByte: end, StartLine: strings.Count(body[:start], "\n") + 1, EndLine: strings.Count(body[:end], "\n") + 1}
	if end > start && body[end-1] == '\n' {
		r.EndLine--
	}
	for _, n := range ix.Nodes {
		if n.Unit != nil && start < n.End && end > n.Start {
			r.UnitRanges = append(r.UnitRanges, *n.Unit)
		}
	}
	return r
}
func display(r Range) string {
	for _, u := range r.UnitRanges {
		if u.Kind == "page" {
			return fmt.Sprintf("第 %d–%d 页", r.UnitRanges[0].Start, r.UnitRanges[len(r.UnitRanges)-1].End)
		}
		if u.Kind == "slide" {
			return fmt.Sprintf("第 %d 张幻灯片", u.Start)
		}
		if u.Kind == "sheet" {
			return "工作表：" + u.Name
		}
	}
	return fmt.Sprintf("第 %d–%d 行", r.StartLine, r.EndLine)
}
