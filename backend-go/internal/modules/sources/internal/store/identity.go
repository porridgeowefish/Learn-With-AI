package store

import (
	"os"
	"path/filepath"
)

type RevisionGuard struct{ SourceID, RevisionID, ContentHash string }

// WithProjectDeletion orders all source/version/job/usage writers with removing
// a project. The callback must not call this store again.
func (s *Store) WithProjectDeletion(remove func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := remove(); err != nil {
		return err
	}
	s.mu.epoch++
	return nil
}

// WithReadableRevisions orders source deletion with a consumer's final commit.
// The callback must not re-enter this store; it should only publish its result.
func (s *Store) WithReadableRevisions(revisions []RevisionGuard, commit func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range revisions {
		if _, _, _, e := s.readRevisionLocked(r.SourceID, r.RevisionID, r.ContentHash); e != nil {
			return e
		}
	}
	return commit()
}

// ReadDerivedRevision pins both the canonical body and its structure sidecar.
// A newer version with the same body hash must not replace frozen provenance.
func (s *Store) ReadDerivedRevision(sourceID, revisionID, versionID, canonicalHash, structureHash string) (Source, Revision, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, rev, body, e := s.readRevisionLocked(sourceID, revisionID, canonicalHash)
	if e != nil {
		return source, rev, body, e
	}
	if versionID == "" || versionID == "legacy" && rev.DerivedVersionID == "legacy" {
		return source, rev, body, nil
	}
	if !safeID(versionID, "derived_") {
		return source, rev, nil, os.ErrNotExist
	}
	var manifest DerivedManifest
	if e = readJSON(filepath.Join(s.root, sourceID, "revisions", revisionID, "derived", "versions", versionID, "manifest.json"), &manifest); e != nil {
		return source, rev, nil, e
	}
	if manifest.SourceRevisionID != revisionID || manifest.OriginalHash != rev.Original.SHA256 || manifest.CanonicalHash != canonicalHash || structureHash != "" && manifest.StructureHash != structureHash {
		return source, rev, nil, ErrRevisionChanged
	}
	rev.DerivedFiles = manifest.Files
	rev.DerivedVersionID = versionID
	ref, ok := canonicalRef(rev)
	if !ok {
		return source, rev, nil, ErrSourceNotReadable
	}
	data, e := os.ReadFile(filepath.Join(s.root, sourceID, "revisions", revisionID, filepath.FromSlash(ref.Path)))
	if e != nil {
		return source, rev, nil, e
	}
	if len(data) > 4<<20 || digest(data) != canonicalHash {
		return source, rev, nil, ErrRevisionChanged
	}
	return source, rev, data, nil
}
