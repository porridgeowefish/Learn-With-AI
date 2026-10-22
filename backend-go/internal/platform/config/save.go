// Section-preserving save for explicit settings flows. The loader never
// writes; only these APIs write, and only behind an explicit user action
// (Settings PUT routes). They round-trip unknown top-level keys untouched.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// pathOverride lets tests redirect the file location.
var pathOverride string
var saveMu sync.Mutex

// UsePathForTest redirects the config file path and returns a restore func.
func UsePathForTest(path string) (restore func()) {
	old := pathOverride
	pathOverride = path
	return func() { pathOverride = old }
}

func activePath(explicit string) string {
	if pathOverride != "" {
		return pathOverride
	}
	return filePath(explicit)
}

// SaveUI writes the ui section, preserving every other top-level key.
func SaveUI(theme string) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	raw, err := readRaw(activePath(""))
	if err != nil {
		return err
	}
	raw["ui"] = map[string]any{"theme": theme}
	return writeRaw(activePath(""), raw)
}

// AISettings mirrors ai.Config for the settings flows.
type AISettings struct {
	Default      string             `json:"default"`
	SearchEngine string             `json:"searchEngine"`
	Providers    []Provider         `json:"providers"`
	Bindings     map[string]Binding `json:"bindings,omitempty"`
}

// SaveAI writes the askAiProviders section under its legacy key so the
// existing local Settings flow and older builds keep reading it. The
// canonical 'ai' section is read-preferred during the compatibility period.
func SaveAI(settings AISettings) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	raw, err := readRaw(activePath(""))
	if err != nil {
		return err
	}
	if existing, ok := raw["ai"]; ok {
		section, _ := existing.(map[string]any)
		if section == nil {
			section = map[string]any{}
		}
		section["defaultProvider"], section["searchEngine"], section["providers"], section["bindings"] = settings.Default, settings.SearchEngine, settings.Providers, settings.Bindings
		raw["ai"] = section
		delete(raw, "askAiProviders")
	} else {
		raw["askAiProviders"] = settings
	}
	return writeRaw(activePath(""), raw)
}

// LoadAI reads the effective AI settings: sectioned 'ai' wins over the
// legacy 'askAiProviders' key.
func LoadAI() (AISettings, bool, error) {
	var out AISettings
	data, err := os.ReadFile(activePath(""))
	if err != nil {
		if os.IsNotExist(err) {
			return out, false, nil
		}
		return out, false, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return out, false, err
	}
	if section, ok := raw["ai"]; ok {
		var ai AI
		if err := json.Unmarshal(section, &ai); err != nil {
			return out, false, err
		}
		out = AISettings{Default: ai.DefaultProvider, SearchEngine: ai.SearchEngine, Providers: ai.Providers, Bindings: ai.Bindings}
		return out, true, nil
	}
	if section, ok := raw["askAiProviders"]; ok {
		if err := json.Unmarshal(section, &out); err != nil {
			return out, false, err
		}
		return out, true, nil
	}
	return out, false, nil
}

// WebSearchSettings is the optional webSearch section backing the teacher's
// search_web tool. An absent section or empty resolved key leaves the tool
// unregistered, so teacher behavior is unchanged.
type WebSearchSettings struct {
	Provider  string `json:"provider"` // "zhipu"
	APIKey    string `json:"apiKey,omitempty"`
	APIKeyEnv string `json:"apiKeyEnv,omitempty"`
	Engine    string `json:"engine,omitempty"` // zhipu engine, e.g. "search_std"
}

// LoadWebSearch reads the webSearch section. ok=false when absent.
func LoadWebSearch() (WebSearchSettings, bool, error) {
	var out WebSearchSettings
	data, err := os.ReadFile(activePath(""))
	if err != nil {
		if os.IsNotExist(err) {
			return out, false, nil
		}
		return out, false, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return out, false, err
	}
	section, ok := raw["webSearch"]
	if !ok {
		return out, false, nil
	}
	if err := json.Unmarshal(section, &out); err != nil {
		return out, false, err
	}
	if out.APIKey == "" && out.APIKeyEnv != "" {
		out.APIKey = strings.TrimSpace(os.Getenv(out.APIKeyEnv))
	}
	return out, true, nil
}

// SaveWebSearch writes the webSearch section, preserving other keys.
func SaveWebSearch(settings WebSearchSettings) error {
	saveMu.Lock()
	defer saveMu.Unlock()
	raw, err := readRaw(activePath(""))
	if err != nil {
		return err
	}
	raw["webSearch"] = settings
	return writeRaw(activePath(""), raw)
}

func readRaw(path string) (map[string]any, error) {
	raw := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return raw, nil
}

func writeRaw(path string, raw map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(path, append(out, '\n')); err != nil {
		return err
	}
	return nil
}

// atomicWrite is the sibling-temp+rename primitive (Windows-safe: same
// volume). Mirrors filesystem.AtomicWriteFile until platform/filesystem
// lands in a later wave; then this delegates.
func atomicWrite(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// SortedKeys is a small helper for deterministic section iteration in tests.
func SortedKeys(raw map[string]any) []string {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var _ = strings.TrimSpace
