package store

import (
	"encoding/json"
	"errors"
	"github.com/xmz14/lll/backend-go/internal/idgen"
	"github.com/xmz14/lll/backend-go/internal/modules/sources/internal/parser"
	"os"
	"path/filepath"
	"time"
)

type StructureRequest struct {
	AllPages             bool   `json:"allPages,omitempty"`
	OperationID          string `json:"operationId"`
	RevisionID           string `json:"revisionId"`
	BaseDerivedVersionID string `json:"baseDerivedVersionId"`
	Pages                []int  `json:"pages,omitempty"`
	ProviderID           string `json:"providerId,omitempty"`
	Model                string `json:"model,omitempty"`
}
type StructureJob struct {
	AllPages               bool                     `json:"allPages,omitempty"`
	CompletedPages         []int                    `json:"completedPages,omitempty"`
	SourceID               string                   `json:"sourceId"`
	ID                     string                   `json:"id"`
	OperationID            string                   `json:"operationId"`
	Status                 string                   `json:"status"`
	RevisionID             string                   `json:"revisionId"`
	BaseDerivedVersionID   string                   `json:"baseDerivedVersionId"`
	ResultDerivedVersionID string                   `json:"resultDerivedVersionId,omitempty"`
	Coverage               parser.StructureCoverage `json:"coverage"`
	ErrorCode              string                   `json:"errorCode,omitempty"`
	CreatedAt              time.Time                `json:"createdAt"`
	UpdatedAt              time.Time                `json:"updatedAt"`
	ProviderID             string                   `json:"providerId"`
	Model                  string                   `json:"model"`
	Pages                  []int                    `json:"pages"`
	EmptyPages             []int                    `json:"emptyPages,omitempty"`
	Automatic              bool                     `json:"automatic,omitempty"`
	OriginalHash           string                   `json:"originalHash"`
	Intent                 StructureRequest         `json:"intent"`
	Requests               []string                 `json:"requestIds,omitempty"`
}

func (s *Store) jobPathLocked(sourceID, jobID string) (string, error) {
	if !safeID(sourceID, "source_") || !safeID(jobID, "structure_") {
		return "", os.ErrNotExist
	}
	var src Source
	if e := readJSON(filepath.Join(s.root, sourceID, "source.json"), &src); e != nil {
		return "", e
	}
	if src.Status == "deleted" || src.Status == "tombstoned" {
		return "", ErrSourceDeleted
	}
	return filepath.Join(s.root, sourceID, "revisions", src.CurrentRevisionID, "structure", "jobs", jobID+".json"), nil
}
func (s *Store) StructureJobs(sourceID string) ([]StructureJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.jobPathLocked(sourceID, "structure_probe")
	if e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(filepath.Dir(p))
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var out []StructureJob
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var j StructureJob
		if readJSON(filepath.Join(filepath.Dir(p), entry.Name()), &j) == nil {
			out = append(out, j)
		}
	}
	return out, nil
}
func (s *Store) SaveStructureJob(sourceID string, j StructureJob) (StructureJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.jobPathLocked(sourceID, j.ID)
	if e != nil {
		return j, e
	}
	var old StructureJob
	if readJSON(p, &old) == nil && (old.Status == "cancelled" || old.Status == "succeeded" || old.Status == "interrupted" || old.Status == "failed") {
		return old, nil
	}
	j.UpdatedAt = time.Now().UTC()
	j.SourceID = sourceID
	return j, writeJSON(p, j)
}
func (s *Store) GetStructureJob(sourceID, jobID string) (StructureJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.jobPathLocked(sourceID, jobID)
	var j StructureJob
	if e != nil {
		return j, e
	}
	e = readJSON(p, &j)
	return j, e
}
func (s *Store) CancelStructureJob(sourceID, jobID string) (StructureJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.jobPathLocked(sourceID, jobID)
	var j StructureJob
	if e != nil {
		return j, e
	}
	if e = readJSON(p, &j); e != nil {
		return j, e
	}
	if j.Status == "queued" || j.Status == "running" {
		j.Status = "cancelled"
		j.UpdatedAt = time.Now().UTC()
		e = writeJSON(p, j)
	}
	return j, e
}

type automaticBudget struct {
	Pages    []int `json:"pages"`
	Requests int   `json:"requests"`
}

// These IDs are reserved by the OCR resolver. A custom cloud provider whose
// name merely ends in "-local-ocr" must not bypass billing or recovery limits.
func IsLocalOCRProvider(providerID string) bool {
	switch providerID {
	case "windows-local-ocr", "macos-local-ocr", "linux-local-ocr", "system-local-ocr":
		return true
	default:
		return false
	}
}

// The debit is durable before dispatch. Cancellation or provider failure does
// not refund a request that may have been billed.
func (s *Store) ReserveStructureRequest(sourceID string, j *StructureJob, page int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.jobPathLocked(sourceID, j.ID)
	if e != nil {
		return e
	}
	var current StructureJob
	if e = readJSON(p, &current); e != nil {
		return e
	}
	if current.Status == "cancelled" {
		return errors.New("cancelled")
	}
	limit := 8
	if j.AllPages {
		limit = 2000
	}
	localWhole := current.AllPages && IsLocalOCRProvider(current.ProviderID)
	if localWhole {
		validPage := false
		for _, candidate := range current.Pages {
			if candidate == page {
				validPage = true
				break
			}
		}
		if !validPage || len(current.Pages) > 2000 {
			return errors.New("invalid-pages")
		}
	}
	// Local retries after a crash consume no paid requests. The bounded page
	// selection remains authoritative even when a checkpoint must be repeated.
	if !localWhole && len(current.Requests) >= limit {
		return errors.New("vision-request-limit")
	}
	if j.Automatic && !IsLocalOCRProvider(j.ProviderID) {
		budgetPath := filepath.Join(filepath.Dir(filepath.Dir(p)), "automatic-budget.json")
		var b automaticBudget
		if e = readJSON(budgetPath, &b); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if b.Requests >= 8 {
			return errors.New("automatic-budget-limit")
		}
		known := false
		for _, n := range b.Pages {
			if n == page {
				known = true
			}
		}
		if !known {
			if len(b.Pages) >= 8 {
				return errors.New("automatic-budget-limit")
			}
			b.Pages = append(b.Pages, page)
		}
		b.Requests++
		if e = writeJSON(budgetPath, b); e != nil {
			return e
		}
	}
	current.Requests = append(current.Requests, idgen.New("request"))
	*j = current
	return writeJSON(p, current)
}
func (s *Store) CommitStructureJob(sourceID, jobID, content string, structure *parser.Structure, images map[string][]byte) (StructureJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.jobPathLocked(sourceID, jobID)
	var j StructureJob
	if e != nil {
		return j, e
	}
	if e = readJSON(p, &j); e != nil {
		return j, e
	}
	if j.Status != "running" {
		return j, errors.New("cancelled")
	}
	rev, e := s.commitEnhancedLocked(sourceID, j.RevisionID, j.BaseDerivedVersionID, j.ID, content, structure, images)
	if e != nil {
		return j, e
	}
	j.Status = "succeeded"
	j.ResultDerivedVersionID = rev.DerivedVersionID
	j.UpdatedAt = time.Now().UTC()
	if structure != nil {
		j.Coverage = structure.Coverage
	}
	return j, writeJSON(p, j)
}

// Checkpoint keeps the job running while publishing readable pages. Cancellation,
// revision changes and deletion share the same lock as the final commit.
func (s *Store) CheckpointStructureJob(sourceID string, staged StructureJob, content string, structure *parser.Structure, images map[string][]byte) (StructureJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.jobPathLocked(sourceID, staged.ID)
	if err != nil {
		return staged, err
	}
	var current StructureJob
	if err = readJSON(p, &current); err != nil {
		return staged, err
	}
	if current.Status != "running" {
		return current, errors.New("cancelled")
	}
	rev, err := s.commitEnhancedLocked(sourceID, current.RevisionID, current.BaseDerivedVersionID, current.ID, content, structure, images)
	if err != nil {
		return current, err
	}
	current.BaseDerivedVersionID = rev.DerivedVersionID
	current.ResultDerivedVersionID = rev.DerivedVersionID
	current.CompletedPages = append([]int(nil), staged.CompletedPages...)
	current.EmptyPages = append([]int(nil), staged.EmptyPages...)
	current.Coverage = structure.Coverage
	current.UpdatedAt = time.Now().UTC()
	return current, writeJSON(p, current)
}
func (s *Store) AppendStructureUsage(sourceID, jobID string, record any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.jobPathLocked(sourceID, jobID); e != nil {
		return e
	}
	data, e := json.Marshal(record)
	if e != nil {
		return e
	}
	path := filepath.Join(filepath.Dir(s.root), "generation", "usage.jsonl")
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(append(data, '\n')); e != nil {
		return e
	}
	return f.Sync()
}

// RecoverStructureJob never replays a request that may have been billed. A
// published manifest is the durable commit evidence even if job terminal state
// was not saved before the process stopped.
func (s *Store) RecoverStructureJob(sourceID, jobID string) (StructureJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.jobPathLocked(sourceID, jobID)
	var j StructureJob
	if e != nil {
		return j, e
	}
	if e = readJSON(p, &j); e != nil {
		return j, e
	}
	if j.Status != "running" {
		return j, nil
	}
	base := filepath.Join(s.root, sourceID, "revisions", j.RevisionID, "derived")
	var ptr derivedPointer
	var m DerivedManifest
	if j.AllPages && IsLocalOCRProvider(j.ProviderID) {
		if readJSON(filepath.Join(base, "current.json"), &ptr) == nil && safeID(ptr.VersionID, "derived_") && readJSON(filepath.Join(base, "versions", ptr.VersionID, "manifest.json"), &m) == nil && m.JobID == j.ID {
			j.BaseDerivedVersionID = ptr.VersionID
		}
		j.Status = "queued"
		j.ErrorCode = ""
		j.UpdatedAt = time.Now().UTC()
		return j, writeJSON(p, j)
	}
	if readJSON(filepath.Join(base, "current.json"), &ptr) == nil && safeID(ptr.VersionID, "derived_") && readJSON(filepath.Join(base, "versions", ptr.VersionID, "manifest.json"), &m) == nil && m.JobID == j.ID {
		j.Status = "succeeded"
		j.ResultDerivedVersionID = m.DerivedVersionID
		j.ErrorCode = ""
		if j.AllPages && len(j.CompletedPages) < len(j.Pages) {
			j.Status = "interrupted"
			j.ErrorCode = "structure-interrupted"
			j.BaseDerivedVersionID = ptr.VersionID
		}
	} else {
		j.Status = "interrupted"
		j.ErrorCode = "structure-interrupted"
	}
	j.UpdatedAt = time.Now().UTC()
	return j, writeJSON(p, j)
}
