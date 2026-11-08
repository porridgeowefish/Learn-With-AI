package projects

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type MapManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	VersionID     string          `json:"versionId"`
	JobID         string          `json:"jobId,omitempty"`
	Format        string          `json:"format"`
	OverviewHash  string          `json:"overviewHash"`
	TopicsHash    string          `json:"topicsHash"`
	Coverage      json.RawMessage `json:"coverage"`
	CreatedAt     time.Time       `json:"createdAt"`
}
type MapPointer struct {
	SchemaVersion int    `json:"schemaVersion"`
	VersionID     string `json:"versionId"`
}

var mapRepositoryLocks sync.Map

func MapLock(root string) *sync.Mutex {
	v, _ := mapRepositoryLocks.LoadOrStore(root, &sync.Mutex{})
	return v.(*sync.Mutex)
}
func ReadMapVersion(root, version string) (MapManifest, []byte, []byte, error) {
	if !validMapVersion(version) {
		return MapManifest{}, nil, nil, errors.New("invalid-map-version")
	}
	dir := filepath.Join(root, "map", "versions", version)
	var manifest MapManifest
	b, e := readCourseFile(filepath.Join(dir, "manifest.json"))
	if e != nil {
		return manifest, nil, nil, e
	}
	if e = json.Unmarshal(b, &manifest); e != nil {
		return manifest, nil, nil, e
	}
	overview, e := readCourseFile(filepath.Join(dir, "overview.md"))
	if e != nil {
		return manifest, nil, nil, e
	}
	topics, e := readCourseFile(filepath.Join(dir, "topics.json"))
	if e != nil {
		return manifest, nil, nil, e
	}
	digest := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	if manifest.VersionID != version || digest(overview) != manifest.OverviewHash || digest(topics) != manifest.TopicsHash {
		return manifest, nil, nil, errors.New("map-recovery-required")
	}
	return manifest, overview, topics, nil
}

// WriteMapVersion writes data before its manifest. Callers publish the pointer
// only while holding MapLock and after checking their expected base version.
func WriteMapVersion(root, id, job, format string, overview, topics []byte, coverage json.RawMessage) error {
	if !validMapVersion(id) {
		return errors.New("invalid-map-version")
	}
	dir := filepath.Join(root, "map", "versions", id)
	if e := os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	if e := filesystem.AtomicWriteFile(filepath.Join(dir, "overview.md"), overview, 0600); e != nil {
		return e
	}
	if e := filesystem.AtomicWriteFile(filepath.Join(dir, "topics.json"), topics, 0600); e != nil {
		return e
	}
	digest := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	b, e := json.MarshalIndent(MapManifest{1, id, job, format, digest(overview), digest(topics), coverage, time.Now().UTC()}, "", "  ")
	if e != nil {
		return e
	}
	return filesystem.AtomicWriteFile(filepath.Join(dir, "manifest.json"), b, 0600)
}
func PublishMapPointer(root, version string) error {
	if !validMapVersion(version) {
		return errors.New("invalid-map-version")
	}
	b, e := json.Marshal(MapPointer{1, version})
	if e != nil {
		return e
	}
	return filesystem.AtomicWriteFile(filepath.Join(root, "map", "current.json"), b, 0600)
}

// PublishCourseMap owns optimistic version comparison and atomic pointer
// publication. Generation validates its candidate before calling this boundary.
func PublishCourseMap(root, base, id, job, format string, overview, topics []byte, coverage json.RawMessage) error {
	mu := MapLock(root)
	mu.Lock()
	defer mu.Unlock()
	var pointer MapPointer
	b, err := os.ReadFile(filepath.Join(root, "map", "current.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if e := json.Unmarshal(b, &pointer); e != nil {
			return e
		}
		if pointer.SchemaVersion != 1 || !validMapVersion(pointer.VersionID) {
			return errors.New("map-recovery-required")
		}
		if _, _, _, e := ReadMapVersion(root, pointer.VersionID); e != nil {
			return e
		}
	}
	if pointer.VersionID != base {
		return errors.New("map-version-conflict")
	}
	if err = WriteMapVersion(root, id, job, format, overview, topics, coverage); err != nil {
		return err
	}
	return PublishMapPointer(root, id)
}

func validMapVersion(id string) bool {
	return len(id) > 0 && len(id) < 160 && regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(id)
}
