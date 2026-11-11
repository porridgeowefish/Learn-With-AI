package workspaceservice

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

const (
	starterSlug  = "learning-methodology"
	starterTitle = "学习的方法论"
)

//go:embed starter-dialogue.json
var starterDialogue []byte
var starterLandingMu sync.Mutex

func initializeStarter(root string) error {
	meta, err := paths.ReadMetadata(root)
	if err != nil || !meta.StarterPending {
		return err
	}
	if err := publishStarterProject(root, meta.ID); err != nil {
		return err
	}
	meta.StarterPending = false
	meta.StarterLanding, err = projects.ProjectExists(starterSlug)
	if err != nil {
		return err
	}
	return paths.StoreJSON(filepath.Join(root, "workspace.json"), meta)
}

// TakeStarterLanding returns the initial project only once, on a browser visit.
func TakeStarterLanding(root string) (string, error) {
	starterLandingMu.Lock()
	defer starterLandingMu.Unlock()
	meta, err := paths.ReadMetadata(root)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil || !meta.StarterLanding {
		return "", err
	}
	exists, err := projects.ProjectExists(starterSlug)
	if err != nil {
		return "", err
	}
	meta.StarterLanding = false
	if err := paths.StoreJSON(filepath.Join(root, "workspace.json"), meta); err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}
	return "/project/" + starterSlug + "/teacher", nil
}

func publishStarterProject(root, workspaceID string) error {
	dir := filepath.Join(root, "projects")
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		return errors.New("projects must be a directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// The stage is a direct child of the locked root and includes its validated ID.
	stage := filepath.Join(root, ".starter-methodology-"+workspaceID)
	if info, err := os.Lstat(stage); err == nil {
		if !info.IsDir() {
			return errors.New("invalid starter staging directory")
		}
		parent, err := filepath.EvalSymlinks(root)
		if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(stage)
		if err != nil {
			return err
		}
		if rel, err := filepath.Rel(parent, resolved); err != nil || rel != filepath.Base(stage) {
			return errors.New("starter staging directory escapes workspace")
		}
		if err := os.RemoveAll(stage); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(entries) != 0 {
		// The starter was published before the marker cleared, or data was restored.
		return nil
	}
	if err := os.Mkdir(stage, 0700); err != nil {
		return err
	}
	if err := projects.StageLearningProject(stage, starterSlug, starterTitle); err != nil {
		return err
	}
	var dialogue struct{ Question, Answer string }
	if err := json.Unmarshal(starterDialogue, &dialogue); err != nil {
		return err
	}
	if err := teacher.StageExampleExchange(stage, starterSlug, "starter-methodology-v1", dialogue.Question, dialogue.Answer); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.Rename(stage, filepath.Join(dir, starterSlug))
}
