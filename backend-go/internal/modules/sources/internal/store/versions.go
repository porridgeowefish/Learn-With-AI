package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/xmz14/lll/backend-go/internal/idgen"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"

	"github.com/xmz14/lll/backend-go/internal/modules/sources/internal/parser"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxDerivedStorage = 512 << 20

var ErrDerivedConflict = errors.New("derived-conflict")

type DerivedManifest struct {
	SchemaVersion    int       `json:"schemaVersion"`
	DerivedVersionID string    `json:"derivedVersionId"`
	SourceRevisionID string    `json:"sourceRevisionId"`
	OriginalHash     string    `json:"originalHash"`
	CanonicalHash    string    `json:"canonicalHash"`
	StructureHash    string    `json:"structureHash,omitempty"`
	ParserVersion    string    `json:"parserVersion"`
	Files            []FileRef `json:"files"`
	JobID            string    `json:"jobId,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}
type derivedPointer struct {
	VersionID string `json:"derivedVersionId"`
}

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}
func canonicalRef(rev Revision) (FileRef, bool) {
	for _, f := range rev.DerivedFiles {
		if f.Key == "content" && strings.HasPrefix(f.MediaType, "text/markdown") {
			return f, true
		}
	}
	return FileRef{}, false
}
func (s *Store) publishDerivedLocked(rev *Revision, files map[string][]byte, types map[string]string, structure *parser.Structure, jobID string) error {
	content, ok := files["content.md"]
	if !ok || len(content) > 4<<20 || !utf8.Valid(content) || !strings.HasPrefix(types["content.md"], "text/markdown") {
		return errors.New("invalid canonical content")
	}
	version := idgen.New("derived")
	base := filepath.Join(s.root, rev.SourceID, "revisions", rev.RevisionID)
	dir := filepath.Join(base, "derived", "versions", version)
	if structure == nil {
		structure = parser.StructureFromMarkdown(string(content), "local")
	}
	structure.SourceRevisionID = rev.RevisionID
	structure.DerivedVersionID = version
	structure.OriginalHash = rev.Original.SHA256
	structure.CanonicalHash = digest(content)
	raw, e := json.Marshal(structure)
	if e != nil {
		return e
	}
	if len(raw) > 16<<20 {
		return errors.New("structure-storage-limit")
	}
	all := map[string][]byte{}
	for k, v := range files {
		all[k] = v
	}
	all["structure.json"] = raw
	var used int64
	walkErr := filepath.WalkDir(filepath.Join(base, "derived"), func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			if errors.Is(e, os.ErrNotExist) {
				return nil
			}
			return e
		}
		if !d.IsDir() {
			if i, e := d.Info(); e == nil {
				used += i.Size()
			}
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	var refs []FileRef
	var imageBytes int
	for key, data := range all {
		if key != "content.md" && key != "structure.json" && !(strings.HasPrefix(key, "images/") && strings.HasSuffix(key, ".png")) {
			return errors.New("invalid derived key")
		}
		clean := filepath.Clean(filepath.FromSlash(key))
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return errors.New("invalid derived path")
		}
		if strings.HasPrefix(key, "images/") {
			if len(data) > 8<<20 {
				return errors.New("image-limit")
			}
			imageBytes += len(data)
		}
		used += int64(len(data))
		if used > MaxDerivedStorage || imageBytes > 64<<20 {
			return errors.New("derived-storage-limit")
		}
		media := types[key]
		if key == "structure.json" {
			media = "application/json"
		}
		if media == "" {
			media = "image/png"
		}
		refs = append(refs, FileRef{Key: strings.TrimSuffix(key, filepath.Ext(key)), Path: filepath.ToSlash(filepath.Join("derived", "versions", version, clean)), MediaType: media, Bytes: int64(len(data)), SHA256: digest(data)})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Key < refs[j].Key })
	// An orphan complete version after a crash is harmless: only current.json publishes it.
	for key, data := range all {
		if e = filesystem.AtomicWriteFile(filepath.Join(dir, filepath.FromSlash(key)), data, 0o644); e != nil {
			return e
		}
	}
	manifest := DerivedManifest{SchemaVersion: 1, DerivedVersionID: version, SourceRevisionID: rev.RevisionID, OriginalHash: rev.Original.SHA256, CanonicalHash: digest(content), StructureHash: digest(raw), ParserVersion: parser.ParserVersion, Files: refs, JobID: jobID, CreatedAt: time.Now().UTC()}
	manifestBytes, e := json.MarshalIndent(manifest, "", "  ")
	if e != nil {
		return e
	}
	if used+int64(len(manifestBytes)) > MaxDerivedStorage {
		return errors.New("derived-storage-limit")
	}
	if e = writeJSON(filepath.Join(dir, "manifest.json"), manifest); e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(base, "derived", "current.json"), derivedPointer{version}); e != nil {
		return e
	}
	rev.DerivedFiles = refs
	rev.DerivedVersionID = version
	return writeJSON(filepath.Join(base, "revision.json"), rev)
}
func (s *Store) resolveDerivedLocked(rev Revision, hash string) (Revision, error) {
	base := filepath.Join(s.root, rev.SourceID, "revisions", rev.RevisionID, "derived")
	var ptr derivedPointer
	if e := readJSON(filepath.Join(base, "current.json"), &ptr); e == nil {
		if !safeID(ptr.VersionID, "derived_") {
			return rev, ErrSourceNotReadable
		}
		var m DerivedManifest
		if e = readJSON(filepath.Join(base, "versions", ptr.VersionID, "manifest.json"), &m); e != nil {
			return rev, e
		}
		rev.DerivedFiles = m.Files
		rev.DerivedVersionID = m.DerivedVersionID
	} else if !errors.Is(e, os.ErrNotExist) {
		return rev, e
	}
	if f, ok := canonicalRef(rev); ok && (hash == "" || f.SHA256 == hash) {
		return rev, nil
	}
	entries, e := os.ReadDir(filepath.Join(base, "versions"))
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return rev, e
	}
	for _, entry := range entries {
		if !entry.IsDir() || !safeID(entry.Name(), "derived_") {
			continue
		}
		var m DerivedManifest
		if readJSON(filepath.Join(base, "versions", entry.Name(), "manifest.json"), &m) == nil && m.SourceRevisionID == rev.RevisionID && m.OriginalHash == rev.Original.SHA256 && m.CanonicalHash == hash {
			rev.DerivedFiles = m.Files
			rev.DerivedVersionID = m.DerivedVersionID
			return rev, nil
		}
	}
	// Legacy canonical stays read-only even after a version upgrade.
	if data, e := os.ReadFile(filepath.Join(base, "content.md")); e == nil && digest(data) == hash {
		rev.DerivedFiles = []FileRef{{Key: "content", Path: "derived/content.md", MediaType: "text/markdown", SHA256: hash, Bytes: int64(len(data))}}
		rev.DerivedVersionID = "legacy"
		return rev, nil
	}
	return rev, ErrRevisionChanged
}
func (s *Store) ReadStructure(sourceID, revisionID, hash string) (*parser.Structure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, rev, body, e := s.readRevisionLocked(sourceID, revisionID, hash)
	if e != nil {
		return nil, e
	}
	for _, ref := range rev.DerivedFiles {
		if ref.Key == "structure" {
			data, e := os.ReadFile(filepath.Join(s.root, sourceID, "revisions", revisionID, filepath.FromSlash(ref.Path)))
			if e != nil {
				return nil, e
			}
			if len(data) > 16<<20 || digest(data) != ref.SHA256 {
				return nil, ErrRevisionChanged
			}
			var st parser.Structure
			if json.Unmarshal(data, &st) != nil || st.CanonicalHash != digest(body) {
				return nil, ErrRevisionChanged
			}
			return &st, nil
		}
	}
	return parser.StructureFromMarkdown(string(body), "local"), nil
}

func (s *Store) ReadDerivedStructure(sourceID, revisionID, versionID, hash, structureHash string) (*parser.Structure, error) {
	_, rev, body, e := s.ReadDerivedRevision(sourceID, revisionID, versionID, hash, structureHash)
	if e != nil {
		return nil, e
	}
	for _, ref := range rev.DerivedFiles {
		if ref.Key == "structure" {
			data, e := os.ReadFile(filepath.Join(s.root, sourceID, "revisions", revisionID, filepath.FromSlash(ref.Path)))
			if e != nil {
				return nil, e
			}
			if len(data) > 16<<20 || digest(data) != ref.SHA256 {
				return nil, ErrRevisionChanged
			}
			var st parser.Structure
			if json.Unmarshal(data, &st) != nil || st.CanonicalHash != digest(body) || st.DerivedVersionID != rev.DerivedVersionID {
				return nil, ErrRevisionChanged
			}
			return &st, nil
		}
	}
	return parser.StructureFromMarkdown(string(body), "local"), nil
}
func (s *Store) CommitStructured(sourceID, revisionID, parserName, content string, truncated bool, structure *parser.Structure) (Source, error) {
	return s.commitParsed(sourceID, revisionID, parserName, content, truncated, structure)
}
func (s *Store) CommitEnhanced(sourceID, revisionID, baseVersion, jobID, content string, structure *parser.Structure, images map[string][]byte) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitEnhancedLocked(sourceID, revisionID, baseVersion, jobID, content, structure, images)
}
func (s *Store) commitEnhancedLocked(sourceID, revisionID, baseVersion, jobID, content string, structure *parser.Structure, images map[string][]byte) (Revision, error) {
	var source Source
	if e := readJSON(filepath.Join(s.root, sourceID, "source.json"), &source); e != nil {
		return Revision{}, e
	}
	if source.Status == "deleted" || source.Status == "tombstoned" {
		return Revision{}, ErrSourceDeleted
	}
	if source.CurrentRevisionID != revisionID {
		return Revision{}, ErrRevisionChanged
	}
	var rev Revision
	if e := readJSON(filepath.Join(s.root, sourceID, "revisions", revisionID, "revision.json"), &rev); e != nil {
		return rev, e
	}
	rev, e := s.resolveDerivedLocked(rev, "")
	if e != nil && source.Status == "ready" {
		return rev, e
	}
	if rev.DerivedVersionID == "" {
		rev.DerivedVersionID = "unparsed"
		if len(rev.DerivedFiles) > 0 {
			rev.DerivedVersionID = "legacy"
		}
	}
	if rev.DerivedVersionID != baseVersion {
		return rev, ErrDerivedConflict
	}
	files := map[string][]byte{"content.md": []byte(content)}
	for k, v := range images {
		files[k] = v
	}
	if e = s.publishDerivedLocked(&rev, files, map[string]string{"content.md": "text/markdown; charset=utf-8"}, structure, jobID); e != nil {
		return rev, e
	}
	source.Status = "ready"
	source.FailureCode = ""
	source.ExtractedBytes = int64(len(content))
	source.UpdatedAt = time.Now().UTC()
	source.Retrieval = nil
	return rev, writeJSON(filepath.Join(s.root, sourceID, "source.json"), source)
}
