package workspaceservice

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/platform/config"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

var errReopenWorkspace = errors.New("reopen relocated workspace")

// OpenRuntime finishes interrupted transactions before any module can write.
func OpenRuntime(opts config.Options, defaultRoot string) (config.Config, *paths.Lease, error) {
	for {
		cfg, lease, err := config.OpenRuntime(opts, defaultRoot, validateLegacyProjectsRoot)
		if err != nil {
			return cfg, nil, err
		}
		err = RecoverMove(lease.Root)
		if err == nil {
			err = RecoverImport(lease.Root)
		}
		if err == nil {
			err = projects.MigrateCourseBindings(filepath.Join(lease.Root, "projects"))
		}
		if err == nil {
			err = initializeStarter(lease.Root)
		}
		if err == nil {
			return cfg, lease, nil
		}
		lease.Close()
		if !errors.Is(err, errReopenWorkspace) {
			return cfg, nil, err
		}
	}
}

// Initial adoption checks the workspace boundary. Individual projects are
// discovered and checked by the project index, including older layouts.
func validateLegacyProjectsRoot(root string, meta *paths.Metadata) error {
	dir := filepath.Join(root, "projects")
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		for _, name := range []string{"preferences.md", "folders.json", "treasures"} {
			if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
				return nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		meta.StarterPending = true
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("projects must be a directory")
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.Type()&os.ModeSymlink != 0 {
			return errors.New("project link rejected")
		}
	}
	return nil
}
