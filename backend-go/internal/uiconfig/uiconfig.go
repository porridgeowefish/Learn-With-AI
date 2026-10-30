// Package uiconfig persists local UI preferences without clobbering other config.
package uiconfig

import (
	"encoding/json"
	"errors"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
)

type Theme string

const (
	ThemeLycheePaper  Theme = "lychee-paper"
	ThemeMountainMist Theme = "mountain-mist"
	ThemeWisteriaGray Theme = "wisteria-gray"
	ThemeCodexWhite   Theme = "codex-white"
	ThemeClaudePaper  Theme = "claude-paper"
)

type Config struct {
	Theme Theme `json:"theme"`
}

func ValidTheme(theme Theme) bool {
	switch theme {
	case ThemeLycheePaper, ThemeMountainMist, ThemeWisteriaGray, ThemeCodexWhite, ThemeClaudePaper:
		return true
	default:
		return false
	}
}

var pathFn = func() string {
	base := filesystem.WORKSPACE
	if base == "" {
		base = filesystem.PROJECT_ROOT
	}
	return filepath.Join(base, "config.local.json")
}

func UseConfigPathForTest(path string) func() {
	old := pathFn
	pathFn = func() string { return path }
	return func() { pathFn = old }
}

func Load() (Config, error) {
	out := Config{Theme: ThemeLycheePaper}
	data, err := os.ReadFile(pathFn())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return out, err
	}
	if section, ok := raw["ui"]; ok {
		if err := json.Unmarshal(section, &out); err != nil {
			return Config{}, err
		}
	}
	if !ValidTheme(out.Theme) {
		out.Theme = ThemeLycheePaper
	}
	return out, nil
}

func Save(cfg Config) error {
	if !ValidTheme(cfg.Theme) {
		return errors.New("unknown theme")
	}
	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(pathFn()); err == nil {
		_ = json.Unmarshal(data, &raw)
	} else if !os.IsNotExist(err) {
		return err
	}
	section, _ := json.Marshal(cfg)
	raw["ui"] = section
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return filesystem.AtomicWriteFile(pathFn(), append(out, '\n'), 0o644)
}
