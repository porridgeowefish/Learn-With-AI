package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var ErrSourceDeleted = errors.New("source_deleted")
var ErrRevisionChanged = errors.New("revision_changed")
var ErrSourceNotReadable = errors.New("source_not_readable")

type RetrievalStatus struct {
	State          string  `json:"state"`
	IndexerVersion string  `json:"indexerVersion,omitempty"`
	Coverage       float64 `json:"coverage,omitempty"`
	Reason         string  `json:"reason,omitempty"`
}

// RetrievalIdentity is a private namespace used to bind opaque handles to a project.
func (s *Store) RetrievalIdentity() string {
	sum := sha256.Sum256([]byte(s.root))
	return hex.EncodeToString(sum[:])
}

func (s *Store) readRevisionLocked(sourceID, revisionID, hash string) (Source, Revision, []byte, error) {
	var source Source
	var revision Revision
	if !safeID(sourceID, "source_") || !safeID(revisionID, "srev_") {
		return source, revision, nil, os.ErrNotExist
	}
	if err := readJSON(filepath.Join(s.root, sourceID, "source.json"), &source); err != nil {
		return source, revision, nil, err
	}
	if source.Status == "deleted" || source.Status == "tombstoned" {
		return source, revision, nil, ErrSourceDeleted
	}
	if source.CurrentRevisionID != revisionID {
		return source, revision, nil, ErrRevisionChanged
	}
	if source.Status != "ready" {
		return source, revision, nil, ErrSourceNotReadable
	}
	path := filepath.Join(s.root, sourceID, "revisions", revisionID)
	if err := readJSON(filepath.Join(path, "revision.json"), &revision); err != nil {
		return source, revision, nil, err
	}
	revision, err := s.resolveDerivedLocked(revision, hash)
	if err != nil {
		return source, revision, nil, err
	}
	ref, ok := canonicalRef(revision)
	if !ok {
		return source, revision, nil, ErrSourceNotReadable
	}
	bodyPath := filepath.Join(path, filepath.FromSlash(ref.Path))
	real, err := filepath.EvalSymlinks(bodyPath)
	if err != nil {
		return source, revision, nil, err
	}
	base, err := filepath.EvalSymlinks(path)
	if err != nil {
		return source, revision, nil, err
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return source, revision, nil, ErrSourceNotReadable
	}
	file, err := os.Open(real)
	if err != nil {
		return source, revision, nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil {
		return source, revision, nil, err
	}
	if len(data) > 4<<20 || !utf8.Valid(data) || len(strings.TrimSpace(string(data))) == 0 {
		return source, revision, nil, ErrSourceNotReadable
	}
	sum := sha256.Sum256(data)
	actual := "sha256:" + hex.EncodeToString(sum[:])
	if actual != ref.SHA256 || hash != "" && actual != hash {
		return source, revision, nil, ErrRevisionChanged
	}
	return source, revision, data, nil
}

// ReadRevision holds the deletion/revision lock and verifies the canonical text hash.
func (s *Store) ReadRevision(sourceID, revisionID, hash string) (Source, Revision, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRevisionLocked(sourceID, revisionID, hash)
}

func validRetrievalKey(key string) bool {
	return key == "index.json" || strings.HasPrefix(key, "evidence_") && strings.HasSuffix(key, ".json") && len(key) < 100 && !strings.ContainsAny(key, `/\`)
}
func (s *Store) ReadRetrieval(sourceID, revisionID, hash, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validRetrievalKey(key) {
		return nil, os.ErrNotExist
	}
	if _, _, _, err := s.readRevisionLocked(sourceID, revisionID, hash); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(s.root, sourceID, "revisions", revisionID, "derived", "retrieval", key))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if len(data) > 32<<20 {
		return nil, errors.New("retrieval cache exceeds limit")
	}
	return data, err
}

// PublishRetrieval revalidates under the parser/deletion lock before atomic publication.
func (s *Store) PublishRetrieval(sourceID, revisionID, hash, key string, data []byte) error {
	if !validRetrievalKey(key) || len(data) > 32<<20 {
		return errors.New("invalid retrieval cache")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, _, err := s.readRevisionLocked(sourceID, revisionID, hash); err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(filepath.Join(s.root, sourceID, "revisions", revisionID, "derived", "retrieval", key), data, 0o600)
}
func (s *Store) SetRetrievalStatus(sourceID, revisionID, hash string, status RetrievalStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, _, _, err := s.readRevisionLocked(sourceID, revisionID, hash)
	if err != nil {
		return err
	}
	source.Retrieval = &status
	return writeJSON(filepath.Join(s.root, sourceID, "source.json"), source)
}
