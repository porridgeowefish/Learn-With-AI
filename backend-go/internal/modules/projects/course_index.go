package projects

import (
	"os"
	"path/filepath"
	"sync"
)

// Keep one bounded workspace metadata index. Directory metadata detects
// creation/deletion; state metadata detects rename/title/type changes. Scope
// bindings remain independently revalidated by the file LRU.
var courseIndex struct {
	sync.Mutex
	root   string
	info   os.FileInfo
	items  []ProjectMeta
	states map[string]os.FileInfo
}

func indexedCourseProjects(slug string) ([]ProjectMeta, error) {
	projectRoot, err := ProjectRootForSlug(slug)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(projectRoot)
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	courseIndex.Lock()
	defer courseIndex.Unlock()
	valid := courseIndex.root == root && courseIndex.info != nil && os.SameFile(courseIndex.info, info) && courseIndex.info.ModTime() == info.ModTime()
	if valid {
		for path, previous := range courseIndex.states {
			current, e := os.Stat(path)
			if e != nil || !os.SameFile(previous, current) || previous.ModTime() != current.ModTime() || previous.Size() != current.Size() {
				valid = false
				break
			}
		}
	}
	if valid {
		return append([]ProjectMeta(nil), courseIndex.items...), nil
	}
	items, err := IndexAll()
	if err != nil {
		return nil, err
	}
	// Refuse to retain unusually large indices; the authoritative disk read still succeeds.
	if len(items) > 4096 {
		courseIndex.root = ""
		courseIndex.items = nil
		courseIndex.states = nil
		return items, nil
	}
	states := map[string]os.FileInfo{}
	for _, item := range items {
		path := filepath.Join(root, item.Slug, "state.json")
		if st, e := os.Stat(path); e == nil {
			states[path] = st
		}
	}
	courseIndex.root = root
	courseIndex.info = info
	courseIndex.items = append([]ProjectMeta(nil), items...)
	courseIndex.states = states
	return items, nil
}
