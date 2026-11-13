package retrieval

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ToolError) Error() string { return e.Code + ": " + e.Message }

type Result struct {
	Status           string     `json:"status"`
	Data             any        `json:"data,omitempty"`
	Error            *ToolError `json:"error,omitempty"`
	Continuation     string     `json:"continuation,omitempty"`
	Truncated        bool       `json:"truncated"`
	TruncationReason string     `json:"truncationReason,omitempty"`
}
type Coverage struct {
	ReadableBytes       int      `json:"readableBytes"`
	ExtractionTruncated bool     `json:"extractionTruncated"`
	CompleteWithinScope bool     `json:"completeWithinScope"`
	OmittedBlocks       []string `json:"omittedBlocks,omitempty"`
	UnreadUnits         []int    `json:"unreadUnits,omitempty"`
	Reasons             []string `json:"reasons,omitempty"`
}
type Document struct {
	DerivedVersionID   string   `json:"derivedVersionId,omitempty"`
	StructureHash      string   `json:"structureHash,omitempty"`
	SourceID           string   `json:"sourceId"`
	RevisionID         string   `json:"revisionId"`
	ContentHash        string   `json:"contentHash"`
	Name               string   `json:"name"`
	Type               string   `json:"type"`
	Readable           bool     `json:"readable"`
	IndexState         string   `json:"indexState"`
	ExtractionCoverage Coverage `json:"extractionCoverage"`
	StructureType      string   `json:"structureType"`
}
type UnitRange struct {
	Kind  string `json:"kind"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
	Name  string `json:"name,omitempty"`
}
type Range struct {
	StartByte  int         `json:"startByte"`
	EndByte    int         `json:"endByte"`
	StartLine  int         `json:"startLine"`
	EndLine    int         `json:"endLine"`
	UnitRanges []UnitRange `json:"unitRanges,omitempty"`
}
type Citation struct {
	DerivedVersionID string      `json:"derivedVersionId,omitempty"`
	StructureHash    string      `json:"structureHash,omitempty"`
	Provenance       string      `json:"provenance,omitempty"`
	EvidenceID       string      `json:"evidenceId"`
	SourceID         string      `json:"sourceId"`
	RevisionID       string      `json:"revisionId"`
	ContentHash      string      `json:"contentHash"`
	StartByte        int         `json:"startByte"`
	EndByte          int         `json:"endByte"`
	StartLine        int         `json:"startLine"`
	EndLine          int         `json:"endLine"`
	UnitRanges       []UnitRange `json:"unitRanges,omitempty"`
	DisplayRange     string      `json:"displayRange"`
	QuoteHash        string      `json:"quoteHash"`
}
type Node struct {
	NodeID     string `json:"nodeId"`
	ParentID   string `json:"parentId,omitempty"`
	Title      string `json:"title"`
	Confidence string `json:"confidence"`
	Locator    string `json:"locator"`
	Range      Range  `json:"range"`
}
type Hit struct {
	SourceID    string   `json:"sourceId"`
	RevisionID  string   `json:"revisionId"`
	HeadingPath []string `json:"headingPath"`
	Preview     string   `json:"preview"`
	Locator     string   `json:"locator"`
	Range       Range    `json:"range"`
	MatchKind   string   `json:"matchKind"`
	EvidenceID  string   `json:"evidenceId"`
	Citation    Citation `json:"citation"`
}
type ListArgs struct {
	Cursor    string `json:"cursor,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	MaxTokens int    `json:"-"`
}
type Target struct {
	Kind  string `json:"kind"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
	Name  string `json:"name,omitempty"`
}
type OutlineArgs struct {
	SourceID   string  `json:"sourceId"`
	RevisionID string  `json:"revisionId"`
	Target     *Target `json:"target,omitempty"`
	Cursor     string  `json:"cursor,omitempty"`
	Limit      int     `json:"limit,omitempty"`
	MaxTokens  int     `json:"-"`
}
type SearchArgs struct {
	Queries   []string `json:"queries"`
	SourceIDs []string `json:"sourceIds,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	MaxTokens int      `json:"-"`
}
type ReadArgs struct {
	SourceID   string `json:"sourceId"`
	RevisionID string `json:"revisionId"`
	Locator    string `json:"locator,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	MaxTokens  int    `json:"maxTokens,omitempty"`
}
type ListData struct {
	Documents []Document `json:"documents"`
}
type OutlineData struct {
	Nodes []Node `json:"nodes"`
}
type SearchData struct {
	Hits []Hit `json:"hits"`
}
type ReadData struct {
	Body        string   `json:"body"`
	Ranges      []Range  `json:"ranges"`
	ScopeRange  Range    `json:"scopeRange"`
	HeadingPath []string `json:"headingPath"`
	Coverage    Coverage `json:"coverage"`
	Previous    string   `json:"previous,omitempty"`
	Next        string   `json:"next,omitempty"`
	EvidenceID  string   `json:"evidenceId"`
	Citation    Citation `json:"citation"`
}
