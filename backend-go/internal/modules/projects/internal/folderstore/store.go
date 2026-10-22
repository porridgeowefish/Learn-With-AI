// Package folderstore manages workspace-global project folders — user-created
// named groupings that hold references (project slugs) to projects. File-backed,
// no DB. Membership is by reference only: projects are never moved on disk, so
// deleting a folder simply returns its projects to "uncategorized".
package folderstore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xmz14/lll/backend-go/internal/modules/projects/internal/workspace"
)

// Folder is one user-created grouping of project slugs (references only).
type Folder struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	SlugOrder      []string `json:"slugOrder"`
	MapProjectSlug string   `json:"mapProjectSlug,omitempty"`
}

// MapFolderSpec connects a discipline-map project to the same folder model
// already used by the sidebar. The map is the folder's overview binding; the
// frontend renders it as an explicit overview row before ordinary child rows.
type MapFolderSpec struct {
	Slug  string
	Title string
}

// Layout is the full workspace-global folder layout.
type Layout struct {
	Folders []Folder `json:"folders"`
}

// Store is a file-backed, workspace-global folder store.
type Store struct {
	mu       sync.Mutex
	filePath string
	data     Layout
}

// New opens (or creates) the workspace folder store. The canonical location
// is <projects root>/folders.json, resolved through the workspace module so
// the test override isolates every store the same way. A legacy root-level
// folders.json is migrated in one copy on first open; a failed copy is a hard
// error — proceeding silently would let the next sync persist an empty ledger
// over the learner's classification (the iteration-17 data-loss bug).
func New() (*Store, error) {
	projectsDir := workspace.ProjectsRoot()
	newPath := filepath.Join(projectsDir, "folders.json")
	legacyPath := filepath.Join(filesystem.WORKSPACE, "folders.json")
	if _, err := os.Stat(newPath); err != nil {
		if data, readErr := os.ReadFile(legacyPath); readErr == nil && len(data) > 0 {
			if err := os.MkdirAll(projectsDir, 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(newPath, data, 0o644); err != nil {
				return nil, err
			}
		}
	}
	s := &Store{filePath: newPath}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = Layout{}
			return nil
		}
		return err
	}
	if len(raw) == 0 {
		s.data = Layout{}
		return nil
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		_, _ = filesystem.BackupCorruptFile(s.filePath, err)
		s.data = Layout{}
		return nil
	}
	return nil
}

func (s *Store) save() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o755); err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(s.filePath, raw, 0o644)
}

// Layout returns a copy of the current folder layout. Folders is always a
// non-nil slice so the JSON serializes as [] rather than null.
func (s *Store) Layout() Layout {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneLayout(s.data)
}

// Replace swaps the entire layout. It sanitizes: drops unnamed folders, caps
// name length, fills missing/duplicate IDs, and enforces tree-style membership
// (a project slug may appear in at most one folder — first occurrence wins).
func (s *Store) Replace(in Layout) (Layout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := Layout{Folders: make([]Folder, 0, len(in.Folders))}
	seenID := map[string]bool{}
	seenSlug := map[string]bool{}
	seenMapSlug := map[string]bool{}
	mapSlugs := map[string]bool{}
	for _, f := range in.Folders {
		mapSlug := strings.TrimSpace(f.MapProjectSlug)
		if workspace.ValidateSlug(mapSlug) {
			mapSlugs[mapSlug] = true
		}
	}
	for _, f := range in.Folders {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		if len([]rune(name)) > 60 {
			name = string([]rune(name)[:60])
		}
		id := strings.TrimSpace(f.ID)
		if id == "" || seenID[id] {
			id = newID()
		}
		seenID[id] = true

		mapSlug := strings.TrimSpace(f.MapProjectSlug)
		if !workspace.ValidateSlug(mapSlug) || seenMapSlug[mapSlug] {
			mapSlug = ""
		} else {
			seenMapSlug[mapSlug] = true
		}

		slugs := make([]string, 0, len(f.SlugOrder))
		for _, sl := range f.SlugOrder {
			sl = strings.TrimSpace(sl)
			if sl == "" || seenSlug[sl] || mapSlugs[sl] {
				continue
			}
			seenSlug[sl] = true
			slugs = append(slugs, sl)
		}
		out.Folders = append(out.Folders, Folder{
			ID: id, Name: name, SlugOrder: slugs, MapProjectSlug: mapSlug,
		})
	}

	s.data = out
	if err := s.save(); err != nil {
		return Layout{}, err
	}
	return cloneLayout(out), nil
}

// RemoveProject drops every workspace-global reference to a deleted project.
// A folder backed by a deleted discipline map is kept as an ordinary folder so
// its learner-owned name and remaining members are not lost.
func (s *Store) RemoveProject(slug string) (Layout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Folders {
		if s.data.Folders[i].MapProjectSlug == slug {
			s.data.Folders[i].MapProjectSlug = ""
		}
		filtered := s.data.Folders[i].SlugOrder[:0]
		for _, member := range s.data.Folders[i].SlugOrder {
			if member != slug {
				filtered = append(filtered, member)
			}
		}
		s.data.Folders[i].SlugOrder = filtered
	}
	if err := s.save(); err != nil {
		return Layout{}, err
	}
	return cloneLayout(s.data), nil
}

// SyncMapFolders makes discipline maps first-class sidebar folders. Existing
// same-name folders are promoted in place so the learner's current
// classification is reused instead of duplicated. Stale map bindings become
// ordinary folders and are never deleted with learner-owned membership.
func (s *Store) SyncMapFolders(specs []MapFolderSpec) (Layout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	valid := make([]MapFolderSpec, 0, len(specs))
	active := map[string]bool{}
	for _, spec := range specs {
		slug := strings.TrimSpace(spec.Slug)
		title := strings.TrimSpace(spec.Title)
		if !workspace.ValidateSlug(slug) || title == "" || active[slug] {
			continue
		}
		active[slug] = true
		valid = append(valid, MapFolderSpec{Slug: slug, Title: title})
	}

	for i := range s.data.Folders {
		if s.data.Folders[i].MapProjectSlug != "" && !active[s.data.Folders[i].MapProjectSlug] {
			s.data.Folders[i].MapProjectSlug = ""
		}
	}

	for _, spec := range valid {
		index := -1
		for i := range s.data.Folders {
			if s.data.Folders[i].MapProjectSlug == spec.Slug {
				index = i
				break
			}
		}
		if index < 0 {
			for i := range s.data.Folders {
				if s.data.Folders[i].MapProjectSlug == "" && strings.EqualFold(strings.TrimSpace(s.data.Folders[i].Name), spec.Title) {
					index = i
					break
				}
			}
		}
		if index < 0 {
			s.data.Folders = append(s.data.Folders, Folder{
				ID: newID(), Name: spec.Title, SlugOrder: []string{}, MapProjectSlug: spec.Slug,
			})
			continue
		}
		s.data.Folders[index].Name = spec.Title
		s.data.Folders[index].MapProjectSlug = spec.Slug
	}

	for i := range s.data.Folders {
		filtered := s.data.Folders[i].SlugOrder[:0]
		for _, slug := range s.data.Folders[i].SlugOrder {
			if !active[slug] {
				filtered = append(filtered, slug)
			}
		}
		s.data.Folders[i].SlugOrder = filtered
	}

	if err := s.save(); err != nil {
		return Layout{}, err
	}
	return cloneLayout(s.data), nil
}

func cloneLayout(in Layout) Layout {
	out := Layout{Folders: make([]Folder, len(in.Folders))}
	for i, folder := range in.Folders {
		out.Folders[i] = folder
		out.Folders[i].SlugOrder = append([]string{}, folder.SlugOrder...)
	}
	return out
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "f_" + hex.EncodeToString(b[:])
}
