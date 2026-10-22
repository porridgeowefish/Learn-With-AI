package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BuildMode is set to release by the portable archive builder.
var BuildMode = "development"

func DiscoverResourceRoot(wd, executable, mode string) (string, error) {
	if mode == "release" {
		resolved, err := filepath.EvalSymlinks(executable)
		if err != nil {
			return "", fmt.Errorf("resolve executable: %w", err)
		}
		root := filepath.Dir(resolved)
		for _, rel := range []string{filepath.Join("frontend", "dist", "index.html"), filepath.Join("learning-prompts", "manifest.json")} {
			if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
				return "", fmt.Errorf("release resource missing: %s", rel)
			}
		}
		return root, nil
	}
	for _, start := range []string{wd, filepath.Dir(executable)} {
		dir := start
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", errors.New("development repository not found; use a portable learn-with-ai release bundle")
}
func DefaultUserWorkspace(goos string, getenv func(string) string, home string) (string, error) {
	switch goos {
	case "windows":
		if local := getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "learn-with-ai"), nil
		}
		if home != "" {
			return filepath.Join(home, "AppData", "Local", "learn-with-ai"), nil
		}
	case "darwin":
		if home != "" {
			return filepath.Join(home, "Library", "Application Support", "learn-with-ai"), nil
		}
	case "linux":
		if xdg := getenv("XDG_DATA_HOME"); xdg != "" && filepath.IsAbs(xdg) {
			return filepath.Join(xdg, "learn-with-ai"), nil
		}
		if home != "" {
			return filepath.Join(home, ".local", "share", "learn-with-ai"), nil
		}
	}
	return "", errors.New("cannot resolve user data directory")
}
