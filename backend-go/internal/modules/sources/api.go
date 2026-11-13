// Package sources is the public facade for source originals and revisions.
package sources

import (
	"context"
	enhancement "github.com/xmz14/lll/backend-go/internal/modules/sources/internal/enhancement"
	ocr "github.com/xmz14/lll/backend-go/internal/modules/sources/internal/ocr"
	processing "github.com/xmz14/lll/backend-go/internal/modules/sources/internal/processing"
	retrieval "github.com/xmz14/lll/backend-go/internal/modules/sources/internal/retrieval"
	impl "github.com/xmz14/lll/backend-go/internal/modules/sources/internal/store"
	"sort"
)

const DefaultFileLimit = impl.DefaultFileLimit
const DefaultUnitByteLimit = impl.DefaultUnitByteLimit

var ErrSourceDeleted = impl.ErrSourceDeleted
var ErrUploadConflict = impl.ErrUploadConflict

type Source = impl.Source
type FileRef = impl.FileRef
type Privacy = impl.Privacy
type Revision = impl.Revision
type Store = impl.Store
type Processor = processing.Processor
type Reader = retrieval.Reader
type SourceDocument = retrieval.Document
type SourceResult = retrieval.Result
type SourceToolError = retrieval.ToolError
type SourceCitation = retrieval.Citation
type SourceRange = retrieval.Range
type SourceCoverage = retrieval.Coverage
type SourceUnitRange = retrieval.UnitRange
type SourceOutlineTarget = retrieval.Target
type ListSourceArgs = retrieval.ListArgs
type OutlineSourceArgs = retrieval.OutlineArgs
type SearchSourceArgs = retrieval.SearchArgs
type ReadSourceArgs = retrieval.ReadArgs
type SourceListData = retrieval.ListData
type SourceOutlineData = retrieval.OutlineData
type SourceSearchData = retrieval.SearchData
type SourceReadData = retrieval.ReadData
type SourceOutlineNode = retrieval.Node
type SourceSearchHit = retrieval.Hit
type RetrievalStatus = impl.RetrievalStatus
type StructureRequest = impl.StructureRequest
type StructureJob = impl.StructureJob
type RevisionGuard = impl.RevisionGuard

var ErrDerivedConflict = impl.ErrDerivedConflict

func ConfigureStructureEvents(event func(string, StructureJob)) { enhancement.ConfigureEvents(event) }
func CreateStructureJob(ctx context.Context, slug, sourceID string, req StructureRequest) (StructureJob, error) {
	return enhancement.Create(ctx, slug, sourceID, req, false)
}
func GetStructureJob(slug, sourceID, jobID string) (StructureJob, error) {
	return enhancement.Get(slug, sourceID, jobID)
}

func ListStructureJobs(slug, sourceID string) ([]StructureJob, error) {
	st, err := impl.New(slug)
	if err != nil {
		return nil, err
	}
	jobs, err := st.StructureJobs(sourceID)
	if err != nil {
		return nil, err
	}
	if jobs == nil {
		jobs = []StructureJob{}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	return jobs, nil
}

type LocalOCRCapability = ocr.LocalCapability

func OCRCapability() LocalOCRCapability { return ocr.LocalPageCapability() }

func ExtractChatImage(ctx context.Context, image []byte, language string) (string, error) {
	return ocr.ExtractChatImage(ctx, image, language)
}

func LocalOCRAvailable() bool { return ocr.LocalPageAvailable() }

func ConfiguredPageOCRAvailable() bool {
	provider, err := ocr.ResolvePageProvider("", "")
	return err == nil && !ocr.IsLocal(provider)
}
func CancelStructureJob(slug, sourceID, jobID string) (StructureJob, error) {
	return enhancement.Cancel(slug, sourceID, jobID)
}
func CancelProjectStructureJobs(slug string) error { return enhancement.CancelProject(slug) }
func CancelSourceStructureJobs(slug, sourceID string) error {
	return enhancement.CancelSource(slug, sourceID)
}

func NewReader(store *Store) *Reader { return retrieval.New(store) }

func NewProcessor(onUpdated func(string, Source)) *Processor { return processing.New(onUpdated) }
func ProcessLocal(ctx context.Context, store *Store, sourceID string) (Source, error) {
	return processing.Process(ctx, store, sourceID)
}

func ParseDisposition(filename, mediaType string) (string, string) {
	return impl.ParseDisposition(filename, mediaType)
}

func New(slug string) (*Store, error) { return impl.New(slug) }

// a usable vision provider.

// ProcessImageOCR extracts text from an uploaded image revision through the
// configured vision model and commits it as the single derived content.md.
func ProcessImageOCR(slug string, store *Store, sourceID, revisionID string) error {
	return ocr.ProcessImageOCR(slug, store, sourceID, revisionID)
}
