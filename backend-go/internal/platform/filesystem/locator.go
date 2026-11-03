package filesystem

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type Metadata struct {
	ID      string `json:"workspaceId"`
	Version int    `json:"workspaceFormatVersion"`
}
type Locator struct {
	Version     int       `json:"locatorVersion"`
	ID          string    `json:"workspaceId"`
	Root        string    `json:"currentRoot"`
	Transaction string    `json:"lastTransactionId"`
	Switched    time.Time `json:"switchedAt"`
}

func NewWorkspaceID() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func ReadMetadata(root string) (Metadata, error) {
	var m Metadata
	b, err := os.ReadFile(filepath.Join(root, "workspace.json"))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	id, e := hex.DecodeString(m.ID)
	if err != nil || e != nil || len(id) != 16 || m.Version != 1 {
		return m, fmt.Errorf("invalid workspace identity or unsupported format in %s", root)
	}
	return m, nil
}
func StoreJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return AtomicWriteFile(path, b, 0600)
}

// ControlDir resolves local runtime discovery and transaction coordination files.
func ControlDir() (string, error) {
	d, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	if runtime.GOOS == "darwin" {
		h, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		d = filepath.Join(h, "Library", "Preferences")
	}
	name := "learn-with-ai"
	if runtime.GOOS == "windows" {
		name = "Learn-With-AI"
	}
	if BuildMode != "release" {
		repo := sha256.Sum256([]byte(PROJECT_ROOT))
		name = filepath.Join(name, "development", hex.EncodeToString(repo[:8]))
	}
	return filepath.Join(d, name), nil
}
func readLocator(dir string) (Locator, error) {
	var l Locator
	b, e := os.ReadFile(filepath.Join(dir, "workspace-locator.json"))
	if e != nil {
		return l, e
	}
	if json.Unmarshal(b, &l) != nil || l.Version != 1 || !filepath.IsAbs(l.Root) || l.ID == "" {
		return l, fmt.Errorf("invalid workspace locator")
	}
	return l, nil
}

type WorkspaceBusyError struct {
	Root string
	Err  error
}

func (e *WorkspaceBusyError) Error() string { return e.Err.Error() }
func (e *WorkspaceBusyError) Unwrap() error { return e.Err }

var writableLeases sync.Map // root -> non-override identity lease
type Lease struct {
	Identity, RootLock *os.File
	Root               string
}

// IsDefault reports whether this process owns the selected default workspace.
func (l *Lease) IsDefault() bool {
	current, ok := writableLeases.Load(l.Root)
	return ok && current == l
}

func (l *Lease) Close() {
	writableLeases.CompareAndDelete(l.Root, l)
	l.RootLock.Close()
	l.Identity.Close()
}

// OpenWorkspace invokes legacy selection only for an initial claim or an explicit override.
func OpenWorkspace(override bool, legacy func() (string, error), validate func(string) error) (*Lease, error) {
	dir, e := ControlDir()
	if e != nil {
		return nil, e
	}
	guard, e := AcquireWorkspaceInstance(dir)
	if e != nil {
		return nil, e
	}
	defer guard.Close()
	loc, e := readLocator(dir)
	exists := e == nil
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	root := loc.Root
	if override || !exists {
		root, e = legacy()
		if e != nil {
			return nil, e
		}
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	m, e := ReadMetadata(root)
	claim := errors.Is(e, os.ErrNotExist) && (!exists || override) && root != loc.Root
	if claim {
		m.ID, e = NewWorkspaceID()
		m.Version = 1
	}
	if e != nil {
		return nil, e
	}
	if exists && ((root == loc.Root && m.ID != loc.ID) || (root != loc.Root && m.ID == loc.ID)) {
		return nil, fmt.Errorf("workspace locator identity conflict: %s", root)
	}
	if _, err := os.Stat(filepath.Join(root, ".workspace-moved")); err == nil && (!exists || root != loc.Root) {
		return nil, fmt.Errorf("workspace moved from %s", root)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	identity, e := AcquireWorkspaceInstance(filepath.Join(dir, "locks", m.ID))
	if e != nil {
		return nil, &WorkspaceBusyError{root, e}
	}
	lock, e := AcquireWorkspaceInstance(root)
	if e != nil {
		identity.Close()
		return nil, &WorkspaceBusyError{root, e}
	}
	lease := &Lease{identity, lock, root}
	if claim && validate != nil {
		e = validate(root)
	}
	if claim && e == nil {
		e = StoreJSON(filepath.Join(root, "workspace.json"), m)
	}
	if e == nil && !exists && !override {
		e = StoreJSON(filepath.Join(dir, "workspace-locator.json"), Locator{1, m.ID, root, "", time.Now().UTC()})
	}
	if e != nil {
		lease.Close()
		return nil, e
	}
	if !override {
		writableLeases.Store(root, lease)
	}
	return lease, nil
}

// BeginMove snapshots the current generation while the running service owns its identity lock.
func BeginMove(root string) (Locator, error) {
	dir, err := ControlDir()
	if err != nil {
		return Locator{}, err
	}
	lock, err := AcquireWorkspaceInstance(dir)
	if err != nil {
		return Locator{}, err
	}
	defer lock.Close()
	if _, ok := writableLeases.Load(root); !ok {
		return Locator{}, fmt.Errorf("workspace selected by runtime override cannot be moved")
	}
	return MoveBaseline(root)
}
func MoveBaseline(root string) (Locator, error) {
	dir, e := ControlDir()
	if e != nil {
		return Locator{}, e
	}
	l, e := readLocator(dir)
	if e == nil && l.Root != root {
		e = fmt.Errorf("workspace is a runtime override")
	}
	return l, e
}
func CommitMove(before Locator, target, tx string) error {
	dir, e := ControlDir()
	if e != nil {
		return e
	}
	lock, e := AcquireWorkspaceInstance(dir)
	if e != nil {
		return e
	}
	defer lock.Close()
	l, e := readLocator(dir)
	if e != nil {
		return e
	}
	if l != before {
		return fmt.Errorf("workspace locator changed")
	}
	l.Root = target
	l.Transaction = tx
	l.Switched = time.Now().UTC()
	return StoreJSON(filepath.Join(dir, "workspace-locator.json"), l)
}
