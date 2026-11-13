package retrieval

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/modules/sources/internal/store"
)

type Reader struct {
	store     *store.Store
	OnUpdated func(store.Source)
}

func New(s *store.Store) *Reader { return &Reader{store: s} }

// EnsureIndex builds only the selected active readable revision; parse readiness is independent.
func (r *Reader) EnsureIndex(ctx context.Context, sourceID string) error {
	source, revision, err := r.store.Get(sourceID)
	if err != nil {
		return err
	}
	if source.Status == "deleted" || source.Status == "tombstoned" {
		return store.ErrSourceDeleted
	}
	if source.Status != "ready" {
		return store.ErrSourceNotReadable
	}
	d := Document{SourceID: sourceID, RevisionID: revision.RevisionID, Name: source.DisplayName, Readable: true, ExtractionCoverage: Coverage{ExtractionTruncated: source.ExtractionTruncated}}
	bindDerived(&d, revision)
	_, _, err = r.load(ctx, d)
	return err
}

func (r *Reader) status(d Document, status store.RetrievalStatus) {
	if r.store.SetRetrievalStatus(d.SourceID, d.RevisionID, d.ContentHash, status) == nil && r.OnUpdated != nil {
		if source, _, err := r.store.Get(d.SourceID); err == nil && source.Status == "ready" {
			r.OnUpdated(source)
		}
	}
}

var signingKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}()
var buildLocks sync.Map

type handle struct {
	DerivedVersion string `json:"d,omitempty"`
	StructureHash  string `json:"g,omitempty"`
	Project        string `json:"p"`
	Source         string `json:"s,omitempty"`
	Revision       string `json:"r,omitempty"`
	Hash           string `json:"h,omitempty"`
	Version        string `json:"v"`
	Kind           string `json:"k"`
	Start          int    `json:"a"`
	End            int    `json:"b"`
	ScopeStart     int    `json:"x,omitempty"`
	ScopeEnd       int    `json:"y,omitempty"`
	Offset         int    `json:"o,omitempty"`
	Snapshot       string `json:"t,omitempty"`
}

func (r *Reader) issue(h handle) string {
	h.Project = r.store.RetrievalIdentity()
	h.Version = indexerVersion
	data, _ := json.Marshal(h)
	mac := hmac.New(sha256.New, signingKey)
	mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (r *Reader) decode(value, kind string) (handle, error) {
	var h handle
	if len(value) > 4096 {
		return h, errors.New("invalid handle")
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return h, errors.New("invalid handle")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return h, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return h, err
	}
	mac := hmac.New(sha256.New, signingKey)
	mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return h, errors.New("invalid handle")
	}
	if json.Unmarshal(data, &h) != nil || h.Project != r.store.RetrievalIdentity() || h.Version != indexerVersion || h.Kind != kind {
		return h, errors.New("invalid handle")
	}
	return h, nil
}
func fail(code string) Result {
	return Result{Status: "error", Error: &ToolError{Code: code, Message: map[string]string{"invalid_arguments": "资料工具参数无效。", "source_not_in_snapshot": "资料不在本轮可用目录。", "source_not_readable": "资料尚不可读取。", "source_deleted": "资料已删除。", "revision_changed": "资料修订已变化，请重新定位。", "invalid_locator": "资料位置无效。", "cursor_expired": "阅读游标已失效，请重新定位。", "cancelled": "查阅已取消。", "budget_exhausted": "本次资料容量不足。", "internal_error": "资料查阅暂时失败。"}[code]}}
}
func codeFor(err error) string {
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return "cancelled"
	case errors.Is(err, store.ErrSourceDeleted):
		return "source_deleted"
	case errors.Is(err, store.ErrRevisionChanged):
		return "revision_changed"
	case errors.Is(err, store.ErrSourceNotReadable):
		return "source_not_readable"
	case errors.Is(err, os.ErrNotExist):
		return "source_not_in_snapshot"
	default:
		return "internal_error"
	}
}
func (r *Reader) Snapshot(ctx context.Context) ([]Document, error) {
	list, err := r.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]Document, 0, len(list))
	for _, source := range list {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if source.Status == "deleted" || source.Status == "tombstoned" {
			continue
		}
		_, rev, err := r.store.Get(source.SourceID)
		if err != nil {
			continue
		}
		d := Document{SourceID: source.SourceID, RevisionID: rev.RevisionID, Name: source.DisplayName, Type: rev.Original.MediaType, IndexState: "absent", StructureType: "lines", ExtractionCoverage: Coverage{ReadableBytes: int(source.ExtractedBytes), ExtractionTruncated: source.ExtractionTruncated}}
		if source.Retrieval != nil {
			d.IndexState = source.Retrieval.State
		}
		bindDerived(&d, rev)
		if source.Status == "ready" {
			_, _, body, err := r.store.ReadRevision(d.SourceID, d.RevisionID, d.ContentHash)
			d.Readable = err == nil
			d.ExtractionCoverage.ReadableBytes = len(body)
			d.ExtractionCoverage.CompleteWithinScope = err == nil && !source.ExtractionTruncated
			structure, structureErr := r.store.ReadDerivedStructure(d.SourceID, d.RevisionID, d.DerivedVersionID, d.ContentHash, d.StructureHash)
			if structureErr != nil {
				d.ExtractionCoverage.CompleteWithinScope = false
			} else {
				d.ExtractionCoverage.CompleteWithinScope = d.ExtractionCoverage.CompleteWithinScope && structure.Coverage.CompleteWithinScope && len(structure.Coverage.UnreadUnits) == 0 && len(structure.Coverage.OmittedBlocks) == 0
				d.ExtractionCoverage.OmittedBlocks = append([]string(nil), structure.Coverage.OmittedBlocks...)
				d.ExtractionCoverage.UnreadUnits = append([]int(nil), structure.Coverage.UnreadUnits...)
				d.ExtractionCoverage.Reasons = append([]string(nil), structure.Coverage.Reasons...)
			}
		}
		switch {
		case strings.Contains(d.Type, "pdf"):
			d.StructureType = "page"
		case strings.Contains(d.Type, "presentation"):
			d.StructureType = "slide"
		case strings.Contains(d.Type, "spreadsheet"):
			d.StructureType = "sheet"
		case strings.Contains(d.Type, "markdown"):
			d.StructureType = "section"
		}
		out = append(out, d)
	}
	return out, nil
}

func bindDerived(d *Document, rev store.Revision) {
	d.DerivedVersionID = rev.DerivedVersionID
	for _, f := range rev.DerivedFiles {
		if f.Key == "content" {
			d.ContentHash = f.SHA256
		}
		if f.Key == "structure" {
			d.StructureHash = f.SHA256
		}
	}
}
func snapshotHash(snapshot []Document) string {
	data, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func find(snapshot []Document, sourceID, revisionID string) (Document, error) {
	for _, d := range snapshot {
		if d.SourceID == sourceID {
			if d.RevisionID != revisionID {
				return d, store.ErrRevisionChanged
			}
			if !d.Readable {
				return d, store.ErrSourceNotReadable
			}
			return d, nil
		}
	}
	return Document{}, os.ErrNotExist
}
func (r *Reader) load(ctx context.Context, d Document) (*index, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	_, _, data, err := r.store.ReadDerivedRevision(d.SourceID, d.RevisionID, d.DerivedVersionID, d.ContentHash, d.StructureHash)
	if err != nil {
		return nil, "", err
	}
	body := string(data)
	key := r.store.RetrievalIdentity() + d.SourceID + d.RevisionID
	mutex, _ := buildLocks.LoadOrStore(key, &sync.Mutex{})
	lock := mutex.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	cached, err := r.store.ReadRetrieval(d.SourceID, d.RevisionID, d.ContentHash, "index.json")
	if err == nil {
		var ix index
		if json.Unmarshal(cached, &ix) == nil && validIndex(&ix, d, len(body)) {
			return &ix, body, nil
		}
	}
	r.status(d, store.RetrievalStatus{State: "building", IndexerVersion: indexerVersion})
	ix, err := buildIndex(ctx, d, body)
	if err != nil {
		r.status(d, store.RetrievalStatus{State: "failed", Reason: codeFor(err)})
		return nil, "", err
	}
	encoded, err := json.Marshal(ix)
	if err != nil {
		return nil, "", err
	}
	if err = ctx.Err(); err != nil {
		r.status(d, store.RetrievalStatus{State: "failed", Reason: "cancelled"})
		return nil, "", err
	}
	if err = r.store.PublishRetrieval(d.SourceID, d.RevisionID, d.ContentHash, "index.json", encoded); err != nil {
		// The cache is disposable. Recheck access after a failed publication,
		// then serve the bounded in-memory index without changing parse readiness.
		if _, _, _, check := r.store.ReadRevision(d.SourceID, d.RevisionID, d.ContentHash); check != nil {
			return nil, "", check
		}
		r.status(d, store.RetrievalStatus{State: "failed", IndexerVersion: indexerVersion, Reason: "cache_unavailable"})
		return ix, body, nil
	}
	state := "ready"
	coverage := 1.0
	if ix.Partial {
		state = "partial"
		coverage = 0
	}
	r.status(d, store.RetrievalStatus{State: state, IndexerVersion: indexerVersion, Coverage: coverage})
	return ix, body, nil
}
func validIndex(ix *index, d Document, bytes int) bool {
	if ix.DerivedVersionID != d.DerivedVersionID || ix.StructureHash != d.StructureHash {
		return false
	}
	if ix.SchemaVersion != 1 || ix.IndexerVersion != indexerVersion || ix.SourceID != d.SourceID || ix.RevisionID != d.RevisionID || ix.ContentHash != d.ContentHash || ix.ReadableBytes != bytes || len(ix.Chunks) == 0 || len(ix.Chunks) > 20000 || len(ix.Nodes) > 10000 {
		return false
	}
	last := 0
	for _, c := range ix.Chunks {
		if c.Start != last || c.End <= c.Start || c.End > bytes {
			return false
		}
		last = c.End
	}
	if last != bytes {
		return false
	}
	for _, n := range ix.Nodes {
		if n.Start < 0 || n.End > bytes || n.Start >= n.End {
			return false
		}
	}
	for _, p := range ix.Postings {
		for i, f := range p {
			if i < 0 || i >= len(ix.Chunks) || f < 1 {
				return false
			}
		}
	}
	return true
}
func limit(value, def, max int) int {
	if value <= 0 {
		return def
	}
	if value > max {
		return max
	}
	return value
}
func (r *Reader) List(ctx context.Context, snapshot []Document, args ListArgs) Result {
	if ctx.Err() != nil {
		return fail("cancelled")
	}
	if args.Limit < 0 || args.Limit > 20 {
		return fail("invalid_arguments")
	}
	offset := 0
	identity := snapshotHash(snapshot)
	if args.Cursor != "" {
		h, err := r.decode(args.Cursor, "list")
		if err != nil || h.Snapshot != identity {
			return fail("cursor_expired")
		}
		offset = h.Offset
	}
	if offset < 0 || offset > len(snapshot) {
		return fail("cursor_expired")
	}
	count := limit(args.Limit, 10, 20)
	budget := limit(args.MaxTokens, 1000, 4000)
	documents := []Document{}
	used := 0
	for offset < len(snapshot) && len(documents) < count {
		encoded, _ := json.Marshal(snapshot[offset])
		cost := EstimateTokens(string(encoded))
		if used+cost > budget {
			break
		}
		documents = append(documents, snapshot[offset])
		used += cost
		offset++
	}
	if len(documents) == 0 && offset < len(snapshot) {
		return fail("budget_exhausted")
	}
	res := Result{Status: "ok", Data: ListData{documents}}
	if offset < len(snapshot) {
		res.Status = "partial"
		res.Truncated = true
		res.TruncationReason = "budget_or_page_limit"
		res.Continuation = r.issue(handle{Kind: "list", Offset: offset, Snapshot: identity})
	}
	return res
}
func (r *Reader) locator(d Document, start, end int) string {
	return r.issue(handle{Kind: "locator", Source: d.SourceID, Revision: d.RevisionID, Hash: d.ContentHash, DerivedVersion: d.DerivedVersionID, StructureHash: d.StructureHash, Start: start, End: end, ScopeStart: start, ScopeEnd: end})
}
func (r *Reader) Outline(ctx context.Context, snapshot []Document, args OutlineArgs) Result {
	if args.Limit < 0 || args.Limit > 50 || args.Target != nil && args.Cursor != "" {
		return fail("invalid_arguments")
	}
	d, err := find(snapshot, args.SourceID, args.RevisionID)
	if err != nil {
		return fail(codeFor(err))
	}
	ix, body, err := r.load(ctx, d)
	if err != nil {
		return fail(codeFor(err))
	}
	nodes := []Node{}
	offset := 0
	start, end := 0, len(body)
	if args.Cursor != "" {
		h, e := r.decode(args.Cursor, "outline")
		if e != nil || h.Source != d.SourceID || h.Revision != d.RevisionID || h.Hash != d.ContentHash || h.DerivedVersion != "" && h.DerivedVersion != d.DerivedVersionID || h.StructureHash != "" && h.StructureHash != d.StructureHash {
			return fail("cursor_expired")
		}
		offset = h.Offset
		start, end = h.Start, h.End
	}
	if args.Target != nil {
		start, end, err = targetRange(ix, body, *args.Target)
		if err != nil {
			return fail("invalid_locator")
		}
		rng := rangeFor(ix, body, start, end)
		node := Node{NodeID: "target", Title: display(rng), Confidence: "extracted", Locator: r.locator(d, start, end), Range: rng}
		nodes = append(nodes, node)
	} else {
		for _, n := range ix.Nodes {
			if n.Start >= start && n.Start < end {
				nodes = append(nodes, Node{NodeID: n.ID, ParentID: n.Parent, Title: n.Title, Confidence: "extracted", Locator: r.locator(d, n.Start, n.End), Range: rangeFor(ix, body, n.Start, n.End)})
			}
		}
		if len(nodes) == 0 {
			for i, c := range ix.Chunks {
				rng := rangeFor(ix, body, c.Start, c.End)
				nodes = append(nodes, Node{NodeID: fmt.Sprintf("lines_%d", i), Title: display(rng), Confidence: "inferred", Locator: r.locator(d, c.Start, c.End), Range: rng})
			}
		}
	}
	if offset < 0 || offset > len(nodes) {
		return fail("cursor_expired")
	}
	count := limit(args.Limit, 20, 50)
	budget := limit(args.MaxTokens, 2000, 4000)
	out := []Node{}
	used := 0
	for offset < len(nodes) && len(out) < count {
		encoded, _ := json.Marshal(nodes[offset])
		cost := EstimateTokens(string(encoded))
		if used+cost > budget {
			break
		}
		used += cost
		out = append(out, nodes[offset])
		offset++
	}
	if len(out) == 0 && offset < len(nodes) {
		return fail("budget_exhausted")
	}
	res := Result{Status: "ok", Data: OutlineData{out}}
	if offset < len(nodes) {
		res.Status = "partial"
		res.Truncated = true
		res.TruncationReason = "budget_or_page_limit"
		res.Continuation = r.issue(handle{Kind: "outline", Source: d.SourceID, Revision: d.RevisionID, Hash: d.ContentHash, DerivedVersion: d.DerivedVersionID, StructureHash: d.StructureHash, Start: start, End: end, Offset: offset})
	}
	markIncompleteCoverage(&res, ix.Partial, ix.ExtractionTruncated)
	return res
}

func markIncompleteCoverage(res *Result, indexPartial, extractionPartial bool) {
	for _, coverage := range []struct {
		partial bool
		reason  string
	}{{indexPartial, "index_coverage"}, {extractionPartial, "extraction_coverage"}} {
		if !coverage.partial {
			continue
		}
		res.Status = "partial"
		res.Truncated = true
		if res.TruncationReason != "" {
			res.TruncationReason += ","
		}
		res.TruncationReason += coverage.reason
	}
}
func targetRange(ix *index, body string, target Target) (int, int, error) {
	invalid := errors.New("invalid target")
	switch target.Kind {
	case "lines":
		if target.Start < 1 || target.End < target.Start {
			return 0, 0, invalid
		}
		pos, line, start, end := 0, 1, -1, -1
		for _, text := range strings.SplitAfter(body, "\n") {
			if line == target.Start {
				start = pos
			}
			pos += len(text)
			if line == target.End {
				end = pos
			}
			line++
		}
		if start < 0 || end < 0 || start >= end {
			return 0, 0, invalid
		}
		return start, end, nil
	case "page", "slide":
		if target.Start < 1 || target.End < target.Start {
			return 0, 0, invalid
		}
		start, end := -1, -1
		count := 0
		for _, n := range ix.Nodes {
			if n.Unit != nil && n.Unit.Kind == target.Kind && n.Unit.Start >= target.Start && n.Unit.End <= target.End {
				if start < 0 {
					start = n.Start
				}
				end = n.End
				count++
			}
		}
		if start < 0 || count != target.End-target.Start+1 {
			return 0, 0, invalid
		}
		return start, end, nil
	case "section", "sheet":
		if strings.TrimSpace(target.Name) == "" || utf8.RuneCountInString(target.Name) > 256 {
			return 0, 0, invalid
		}
		for _, n := range ix.Nodes {
			if target.Kind == "section" && strings.EqualFold(n.Title, target.Name) || target.Kind == "sheet" && n.Unit != nil && n.Unit.Kind == "sheet" && n.Unit.Name == target.Name {
				return n.Start, n.End, nil
			}
		}
	}
	return 0, 0, invalid
}
func (r *Reader) Search(ctx context.Context, snapshot []Document, args SearchArgs) Result {
	if len(args.Queries) < 1 || len(args.Queries) > 3 || len(args.SourceIDs) > 20 || args.Limit < 0 || args.Limit > 6 {
		return fail("invalid_arguments")
	}
	for _, q := range args.Queries {
		if strings.TrimSpace(q) == "" || utf8.RuneCountInString(q) > 256 {
			return fail("invalid_arguments")
		}
	}
	selected := map[string]bool{}
	for _, id := range args.SourceIDs {
		known := false
		for _, d := range snapshot {
			if d.SourceID == id {
				known = true
				selected[id] = true
			}
		}
		if !known {
			return fail("source_not_in_snapshot")
		}
	}
	type candidate struct {
		d     Document
		ix    *index
		body  string
		c     chunk
		score float64
	}
	var candidates []candidate
	indexPartial, extractionPartial := false, false
	for _, d := range snapshot {
		if !d.Readable || len(selected) > 0 && !selected[d.SourceID] {
			continue
		}
		ix, body, err := r.load(ctx, d)
		if err != nil {
			return fail(codeFor(err))
		}
		indexPartial = indexPartial || ix.Partial
		extractionPartial = extractionPartial || ix.ExtractionTruncated
		ranked := rank(ix, args.Queries, d.Name)
		for _, hit := range ranked[:min(len(ranked), 6)] {
			candidates = append(candidates, candidate{d, ix, body, ix.Chunks[hit.Index], hit.Score})
		}
		// Keep only the global winning window. The number of uploaded documents
		// must not cause all canonical bodies and indexes to stay resident.
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
		if len(candidates) > 6 {
			candidates = append([]candidate(nil), candidates[:6]...)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	count := limit(args.Limit, 4, 6)
	budget := limit(args.MaxTokens, 1500, 4000)
	hits := []Hit{}
	remaining := budget
	for _, c := range candidates {
		if len(hits) >= count || remaining < 40 {
			break
		}
		size := min(180, remaining)
		start, end := previewRange(c.body, c.c, args.Queries, size)
		var fits bool
		start, end, fits = alignPreview(c.body, start, end, remaining)
		if !fits {
			continue
		}
		quote := c.body[start:end]
		citation, err := r.evidence(ctx, c.d, c.ix, c.body, start, end)
		if err != nil {
			return fail(codeFor(err))
		}
		hits = append(hits, Hit{SourceID: c.d.SourceID, RevisionID: c.d.RevisionID, HeadingPath: c.c.Path, Preview: quote, Locator: r.locator(c.d, start, c.c.End), Range: rangeFor(c.ix, c.body, start, end), MatchKind: "lexical", EvidenceID: citation.EvidenceID, Citation: citation})
		remaining -= EstimateTokens(quote)
	}
	res := Result{Status: "ok", Data: SearchData{hits}}
	if len(hits) < min(count, len(candidates)) {
		res.Status = "partial"
		res.Truncated = true
		res.TruncationReason = "budget_exhausted"
	}
	markIncompleteCoverage(&res, indexPartial, extractionPartial)
	return res
}
func previewRange(body string, c chunk, queries []string, budget int) (int, int) {
	start := c.Start
	var normalized strings.Builder
	var offsets []int
	for offset, original := range body[c.Start:c.End] {
		lowerRune := unicode.ToLower(original)
		normalized.WriteRune(lowerRune)
		for i := 0; i < utf8.RuneLen(lowerRune); i++ {
			offsets = append(offsets, offset)
		}
	}
	lower := normalized.String()
	best := -1
	for _, q := range queries {
		for _, term := range terms(q) {
			pos := strings.Index(lower, term)
			if pos >= 0 && (best < 0 || offsets[pos] < best) {
				best = offsets[pos]
			}
		}
	}
	if best >= 0 {
		match := c.Start + best
		start = max(c.Start, match-max(1, budget/3)*3)
		for start < match && !utf8.RuneStart(body[start]) {
			start++
		}
		for start < match && EstimateTokens(body[start:match]) > max(1, budget/3) {
			_, size := utf8.DecodeRuneInString(body[start:match])
			start += size
		}
	}
	end := start + tokenPrefix(body[start:c.End], budget)
	return start, end
}
func (r *Reader) Read(ctx context.Context, snapshot []Document, args ReadArgs) Result {
	if (args.Locator == "") == (args.Cursor == "") || args.MaxTokens < 0 || args.MaxTokens > 4000 {
		return fail("invalid_arguments")
	}
	d, err := find(snapshot, args.SourceID, args.RevisionID)
	if err != nil {
		return fail(codeFor(err))
	}
	ix, body, err := r.load(ctx, d)
	if err != nil {
		return fail(codeFor(err))
	}
	kind, value := "locator", args.Locator
	if args.Cursor != "" {
		kind, value = "read", args.Cursor
	}
	h, err := r.decode(value, kind)
	if err != nil || h.Source != d.SourceID || h.Revision != d.RevisionID || h.Hash != d.ContentHash || h.DerivedVersion != "" && h.DerivedVersion != d.DerivedVersionID || h.StructureHash != "" && h.StructureHash != d.StructureHash {
		return fail(map[bool]string{true: "cursor_expired", false: "invalid_locator"}[kind == "read"])
	}
	if !validRange(body, h.Start, h.End) || h.ScopeStart < 0 || h.ScopeEnd > len(body) || h.ScopeStart > h.Start || h.ScopeEnd < h.End {
		return fail("invalid_locator")
	}
	budget := limit(args.MaxTokens, 2000, 4000)
	end := h.Start + tokenPrefix(body[h.Start:h.End], budget)
	end = safeEnd(body, h.Start, end)
	if end == h.Start {
		for _, b := range protectedBlocks(body) {
			if b.Start <= h.Start && h.Start < b.End {
				_, next := r.adjacent(d, body, h.ScopeStart, h.ScopeEnd, h.Start, b.End, budget)
				data := ReadData{Body: "", ScopeRange: rangeFor(ix, body, h.ScopeStart, h.ScopeEnd), Coverage: Coverage{ReadableBytes: len(body), CompleteWithinScope: false, OmittedBlocks: []string{b.ID}}, Next: next}
				return Result{Status: "partial", Data: data, Continuation: next, Truncated: true, TruncationReason: "omitted-structure-block"}
			}
		}
	}
	if end == h.Start {
		return fail("budget_exhausted")
	}
	citation, err := r.evidence(ctx, d, ix, body, h.Start, end)
	if err != nil {
		return fail(codeFor(err))
	}
	previous, next := r.adjacent(d, body, h.ScopeStart, h.ScopeEnd, h.Start, end, budget)
	var path []string
	for _, c := range ix.Chunks {
		if h.Start >= c.Start && h.Start < c.End {
			path = c.Path
			break
		}
	}
	data := ReadData{Body: body[h.Start:end], Ranges: []Range{rangeFor(ix, body, h.Start, end)}, ScopeRange: rangeFor(ix, body, h.ScopeStart, h.ScopeEnd), HeadingPath: path, Coverage: Coverage{ReadableBytes: len(body), ExtractionTruncated: ix.ExtractionTruncated, CompleteWithinScope: end == h.ScopeEnd && !ix.ExtractionTruncated}, Previous: previous, Next: next, EvidenceID: citation.EvidenceID, Citation: citation}
	res := Result{Status: "ok", Data: data}
	if end < h.End {
		res.Status = "partial"
		res.Truncated = true
		res.TruncationReason = "budget_exhausted"
		res.Continuation = next
	}
	if ix.ExtractionTruncated {
		res.Truncated = true
		if res.TruncationReason == "" {
			res.TruncationReason = "extraction_coverage"
		}
	}
	return res
}
func validRange(body string, start, end int) bool {
	return start >= 0 && end > start && end <= len(body) && (start == len(body) || utf8.RuneStart(body[start])) && (end == len(body) || utf8.RuneStart(body[end]))
}
func (r *Reader) adjacent(d Document, body string, scopeStart, scopeEnd, start, end, budget int) (string, string) {
	previous, next := "", ""
	if end < scopeEnd {
		next = r.issue(handle{Kind: "read", Source: d.SourceID, Revision: d.RevisionID, Hash: d.ContentHash, DerivedVersion: d.DerivedVersionID, StructureHash: d.StructureHash, Start: end, End: scopeEnd, ScopeStart: scopeStart, ScopeEnd: scopeEnd})
	}
	if start > scopeStart {
		prevStart := max(scopeStart, start-budget*3)
		for prevStart < start && !utf8.RuneStart(body[prevStart]) {
			prevStart++
		}
		previous = r.issue(handle{Kind: "read", Source: d.SourceID, Revision: d.RevisionID, Hash: d.ContentHash, DerivedVersion: d.DerivedVersionID, StructureHash: d.StructureHash, Start: prevStart, End: start, ScopeStart: scopeStart, ScopeEnd: start})
	}
	return previous, next
}
func (r *Reader) Continuation(ctx context.Context, snapshot []Document, sourceID, revisionID, hash string, startByte, endByte int) (string, error) {
	d, err := find(snapshot, sourceID, revisionID)
	if err != nil {
		return "", err
	}
	if d.ContentHash != hash {
		return "", store.ErrRevisionChanged
	}
	_, body, err := r.load(ctx, d)
	if err != nil {
		return "", err
	}
	if !validRange(body, startByte, endByte) {
		return "", fail("invalid_locator").Error
	}
	return r.issue(handle{Kind: "read", Source: sourceID, Revision: revisionID, Hash: hash, DerivedVersion: d.DerivedVersionID, StructureHash: d.StructureHash, Start: startByte, End: endByte, ScopeStart: startByte, ScopeEnd: endByte}), nil
}
func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (r *Reader) evidence(ctx context.Context, d Document, ix *index, body string, start, end int) (Citation, error) {
	if err := ctx.Err(); err != nil {
		return Citation{}, err
	}
	rng := rangeFor(ix, body, start, end)
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return Citation{}, err
	}
	citation := Citation{EvidenceID: "evidence_" + hex.EncodeToString(raw), SourceID: d.SourceID, RevisionID: d.RevisionID, ContentHash: d.ContentHash, StartByte: start, EndByte: end, StartLine: rng.StartLine, EndLine: rng.EndLine, UnitRanges: rng.UnitRanges, DisplayRange: display(rng), QuoteHash: hashText(body[start:end])}
	citation.DerivedVersionID = d.DerivedVersionID
	citation.StructureHash = d.StructureHash
	citation.Provenance = "local"
	if st, e := r.store.ReadDerivedStructure(d.SourceID, d.RevisionID, d.DerivedVersionID, d.ContentHash, d.StructureHash); e == nil {
		for _, u := range st.Units {
			for _, b := range u.Blocks {
				if start < b.Range.End && end > b.Range.Start && b.Provenance != "local" {
					citation.Provenance = b.Provenance
				}
			}
		}
	}
	encoded, _ := json.Marshal(citation)
	err := r.store.PublishRetrieval(d.SourceID, d.RevisionID, d.ContentHash, citation.EvidenceID+".json", encoded)
	return citation, err
}
func (r *Reader) readCitation(ctx context.Context, sourceID, evidenceID string) (Citation, error) {
	if err := ctx.Err(); err != nil {
		return Citation{}, err
	}
	source, revision, err := r.store.Get(sourceID)
	if err != nil {
		return Citation{}, err
	}
	if source.Status == "deleted" || source.Status == "tombstoned" {
		return Citation{}, store.ErrSourceDeleted
	}
	if !strings.HasPrefix(evidenceID, "evidence_") || len(evidenceID) != 45 {
		return Citation{}, os.ErrNotExist
	}
	raw, err := r.store.ReadRetrieval(sourceID, revision.RevisionID, "", evidenceID+".json")
	if err != nil {
		return Citation{}, err
	}
	var citation Citation
	if json.Unmarshal(raw, &citation) != nil || citation.EvidenceID != evidenceID || citation.SourceID != sourceID {
		return Citation{}, os.ErrNotExist
	}
	_, _, body, err := r.store.ReadDerivedRevision(sourceID, citation.RevisionID, citation.DerivedVersionID, citation.ContentHash, citation.StructureHash)
	if err != nil {
		return Citation{}, err
	}
	text := string(body)
	if !validRange(text, citation.StartByte, citation.EndByte) || hashText(text[citation.StartByte:citation.EndByte]) != citation.QuoteHash {
		return Citation{}, store.ErrRevisionChanged
	}
	return citation, nil
}
func (r *Reader) ValidateCitation(ctx context.Context, citation Citation) error {
	saved, err := r.readCitation(ctx, citation.SourceID, citation.EvidenceID)
	if err != nil {
		return err
	}
	expected, _ := json.Marshal(saved)
	provided, _ := json.Marshal(citation)
	if !hmac.Equal(expected, provided) {
		return fail("invalid_locator").Error
	}
	return nil
}
