package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrLegacyFormat = errors.New("legacy-format-unsupported")
var ErrInvalidLayout = errors.New("project-layout-invalid")

// checkLayout only inspects existing metadata. It never initializes or migrates.
func checkLayout(root string, state *ProjectState) error {
	if !ValidateProjectType(state.ProjectType) || state.LayoutVersion < 0 || state.LayoutVersion > 1 {
		return ErrInvalidLayout
	}
	read := func(name string) (map[string]any, bool, error) {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		if err != nil {
			return nil, true, ErrInvalidLayout
		}
		var value map[string]any
		if len(raw) > 1<<20 || json.Unmarshal(raw, &value) != nil || value["schemaVersion"] != float64(1) {
			return nil, true, ErrInvalidLayout
		}
		return value, true, nil
	}
	if state.ProjectType == ProjectTypeDisciplineMap {
		pointer, present, err := read("map/current.json")
		if err != nil {
			return err
		}
		if present {
			id, ok := pointer["versionId"].(string)
			if !ok || !safeLayoutID(id) {
				return ErrInvalidLayout
			}
			manifest, exists, err := read("map/versions/" + id + "/manifest.json")
			if err != nil || !exists || manifest["versionId"] != id {
				return ErrInvalidLayout
			}
			for _, name := range []string{"overview.md", "topics.json"} {
				if _, err := os.Stat(filepath.Join(root, "map", "versions", id, name)); err != nil {
					return ErrInvalidLayout
				}
			}
			return nil
		}
	} else {
		unit, hasUnit, err := read("unit.json")
		if err != nil {
			return err
		}
		conversation, hasConversation, err := read("conversation/conversation.json")
		if err != nil {
			return err
		}
		if hasUnit || hasConversation {
			if !hasUnit || !hasConversation || unit["unitId"] == "" || unit["unitId"] == nil || unit["conversationId"] == "" || unit["conversationId"] == nil || conversation["id"] != unit["conversationId"] || conversation["unitId"] != unit["unitId"] || unit["projectSlug"] != state.Slug {
				return ErrInvalidLayout
			}
			return nil
		}
		for _, key := range []string{"review", "practice"} {
			meta, present, err := read("assets/" + key + "/asset.json")
			if err != nil {
				return err
			}
			if present {
				id, ok := meta["currentVersionId"].(string)
				if !ok || !safeLayoutID(id) {
					return ErrInvalidLayout
				}
				version, exists, err := read("assets/" + key + "/versions/" + id + "/version.json")
				if err != nil || !exists || version["versionId"] != id {
					return ErrInvalidLayout
				}
				return nil
			}
		}
	}
	if state.LayoutVersion == 1 {
		return nil
	}
	// Old empty placeholders are safe to initialize. Existing lesson content is not.
	for _, name := range []string{"intro", "explain", "practice", "assets/intro", "assets/body", "assets/generated", "assistant-tasks", "runs"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(name)), func(p string, e os.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return ErrInvalidLayout
			}
			if e.Type()&os.ModeSymlink != 0 {
				return ErrInvalidLayout
			}
			if e.IsDir() {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return ErrInvalidLayout
			}
			if info.Size() > 0 {
				return ErrLegacyFormat
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if state.ProjectType == ProjectTypeDisciplineMap {
		raw, err := os.ReadFile(filepath.Join(root, "overview.md"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrInvalidLayout
		}
		text := strings.TrimSpace(string(raw))
		placeholder := strings.TrimSpace("# " + state.Title + "：学科总览\n\n当前总览尚未生成。点击“生成学科总览”，了解学科边界、主要领域、研究方法和典型应用。")
		if text != "" && text != placeholder {
			return ErrLegacyFormat
		}
		raw, err = os.ReadFile(filepath.Join(root, "discipline-topics.json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrInvalidLayout
		}
		if strings.TrimSpace(string(raw)) != "" {
			var catalog struct {
				SchemaVersion int               `json:"schemaVersion"`
				Topics        []json.RawMessage `json:"topics"`
			}
			if json.Unmarshal(raw, &catalog) != nil || catalog.SchemaVersion != 1 || len(catalog.Topics) > 0 {
				return ErrLegacyFormat
			}
		}
	}
	return nil
}

func safeLayoutID(id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > 160 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func layoutStatus(root string, state *ProjectState) string {
	err := checkLayout(root, state)
	if errors.Is(err, ErrLegacyFormat) {
		return "legacy-format-unsupported"
	}
	if err != nil {
		return "project-layout-invalid"
	}
	return "current"
}
