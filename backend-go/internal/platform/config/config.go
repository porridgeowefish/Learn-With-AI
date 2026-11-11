// Package config owns the single typed application configuration loader.
//
// Precedence: compiled defaults < config.local.json < LWA_* environment
// < explicit CLI flags. Loading normalizes the historical flat keys in
// memory and never rewrites the user's file. Diagnostics redact secrets.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/platform/envname"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

// Section shapes. These mirror DATA_DESIGN.md canonical names; the JSON
// tags are the canonical sectioned keys.

type Server struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Workspace struct {
	Root string `json:"root"`
}

type Provider struct {
	MaxConcurrency      int    `json:"maxConcurrency,omitempty"`
	ID                  string `json:"id"`
	Kind                string `json:"kind"` // "openai" | "anthropic"
	Name                string `json:"name"`
	BaseURL             string `json:"baseURL"`
	APIKey              string `json:"apiKey,omitempty"` // compatibility inline key; apiKeyEnv wins
	APIKeyEnv           string `json:"apiKeyEnv,omitempty"`
	Model               string `json:"model"`
	ContextWindowTokens int    `json:"contextWindowTokens,omitempty"`
	Reasoning           bool   `json:"reasoning,omitempty"`
	Thinking            bool   `json:"thinking,omitempty"`
}

type Binding struct {
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
}

type AI struct {
	DefaultProvider string             `json:"defaultProvider"`
	SearchEngine    string             `json:"searchEngine"` // "google" | "bing"
	Providers       []Provider         `json:"providers"`
	Bindings        map[string]Binding `json:"bindings,omitempty"`
}

type UI struct {
	Theme string `json:"theme"`
}

// Config is the resolved typed application configuration.
type Config struct {
	Server    Server    `json:"server"`
	Workspace Workspace `json:"workspace"`
	AI        AI        `json:"ai"`
	UI        UI        `json:"ui"`

	// Path is the config file that was loaded ("" when none existed).
	Path string `json:"-"`
	// Warnings carry non-secret diagnostics (e.g. old+new key conflicts).
	Warnings []string `json:"-"`
}

// source records where each resolved value came from, for diagnostics only.
type source struct {
	Section string `json:"section"`
	From    string `json:"from"` // "default" | "file" | "env" | "flag"
}

// Provenance maps section name → source. Never written back to disk.
type Provenance map[string]source

// Defaults returns the compiled default configuration.
func Defaults() Config {
	return Config{
		Server:    Server{Host: "127.0.0.1", Port: 8787},
		Workspace: Workspace{Root: ""},
		AI:        AI{SearchEngine: "google"},
		UI:        UI{Theme: "lychee-paper"},
	}
}

// Validate reports actionable errors for invalid types, enums, and unsafe
// workspace paths. Secrets are never included in the message.
func (c *Config) Validate() error {
	if c.Server.Port < 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port %d is out of range", c.Server.Port)
	}
	if c.Workspace.Root != "" {
		if !filepath.IsAbs(c.Workspace.Root) {
			return fmt.Errorf("workspace.root %q must be an absolute path", c.Workspace.Root)
		}
		if strings.Contains(c.Workspace.Root, "..") {
			return fmt.Errorf("workspace.root %q must not contain '..'", filepath.ToSlash(c.Workspace.Root))
		}
	}
	if c.AI.SearchEngine != "" && c.AI.SearchEngine != "google" && c.AI.SearchEngine != "bing" {
		return fmt.Errorf("ai.searchEngine %q is not one of google|bing", c.AI.SearchEngine)
	}
	seen := map[string]bool{}
	for i, p := range c.AI.Providers {
		if p.Kind != "openai" && p.Kind != "anthropic" && p.Kind != "siwc" {
			return fmt.Errorf("ai.providers[%d].kind %q is not one of openai|anthropic|siwc", i, p.Kind)
		}
		if p.ID == "" {
			return fmt.Errorf("ai.providers[%d].id is required", i)
		}
		if seen[p.ID] {
			return fmt.Errorf("ai.providers[%d].id %q is duplicated", i, p.ID)
		}
		seen[p.ID] = true
	}
	switch c.UI.Theme {
	case "lychee-paper", "mountain-mist", "wisteria-gray", "codex-white", "claude-paper":
	default:
		return fmt.Errorf("ui.theme %q is not a supported theme", c.UI.Theme)
	}
	return nil
}

// ResolveAPIKey returns the provider API key with environment precedence:
// the variable named by apiKeyEnv wins over an inline apiKey.
func (p Provider) ResolveAPIKey() string {
	if p.APIKeyEnv != "" {
		if v := strings.TrimSpace(envname.Value(p.APIKeyEnv)); v != "" {
			return v
		}
	}
	return p.APIKey
}

// Redacted returns a copy safe for logs: every secret-bearing field is
// replaced with a mask; nothing else changes.
func (c Config) Redacted() Config {
	out := c
	mask := "••••"
	out.AI.Providers = make([]Provider, len(c.AI.Providers))
	for i, p := range c.AI.Providers {
		if p.APIKey != "" {
			p.APIKey = mask
		}
		out.AI.Providers[i] = p
	}
	return out
}

// Options controls Load.
type Options struct {
	// Path overrides the config file location (CLI --config).
	Path string
	// WorkspaceRoot overrides the workspace root (CLI --workspace / env).
	WorkspaceRoot string
	// Port overrides the server port (CLI --port / env).
	Port int
}

// filePath resolves the config file location.
func filePath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return filepath.Join(workspaceBase(), "config.local.json")
}

func workspaceBase() string {
	if v := strings.TrimSpace(envname.Value("LWA_WORKSPACE_ROOT")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("WORKSPACE")); v != "" {
		return v
	}
	// A test binary without an explicit override must stay in its scratch
	// workspace: config.local.json holds the learner's real API keys.
	if paths.TestScratchWorkspace != "" {
		return paths.TestScratchWorkspace
	}
	// Single derivation shared with the filesystem package: without an
	// override the workspace is <repo>/workspace, so the config file lookup
	// and the runtime path variables can never disagree (LESSONS_LEARNED #16).
	return paths.DefaultWorkspaceRoot(paths.PROJECT_ROOT)
}

// diskShape is the raw sectioned document plus tolerated legacy flat keys.
type diskShape struct {
	Server    *json.RawMessage `json:"server"`
	Workspace *json.RawMessage `json:"workspace"`
	AI        *json.RawMessage `json:"ai"`
	UI        *json.RawMessage `json:"ui"`

	// Legacy flat keys (read-compatible only).
	AskAiProviders *json.RawMessage `json:"askAiProviders"`
}

// legacyAI mirrors ai.Config.
type legacyAI struct {
	Default      string             `json:"default"`
	SearchEngine string             `json:"searchEngine"`
	Providers    []legacyProvider   `json:"providers"`
	Bindings     map[string]Binding `json:"bindings"`
}

type legacyProvider struct {
	ID                  string `json:"id"`
	Kind                string `json:"kind"`
	Name                string `json:"name"`
	BaseURL             string `json:"baseURL"`
	APIKey              string `json:"apiKey"`
	Model               string `json:"model"`
	ContextWindowTokens int    `json:"contextWindowTokens,omitempty"`
	Reasoning           bool   `json:"reasoning,omitempty"`
	Thinking            bool   `json:"thinking,omitempty"`
}

// Load resolves the typed configuration. It reads the file (when present),
// applies compatibility mapping, then environment, then explicit options.
// It never writes.
func Load(opts Options) (Config, Provenance, error) {
	cfg := Defaults()
	prov := Provenance{}
	for _, section := range []string{"server", "workspace", "ai", "ui"} {
		prov[section] = source{Section: section, From: "default"}
	}

	path := filePath(opts.Path)
	cfg.Path = path
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, prov, fmt.Errorf("read %s: %w", path, err)
	}
	if err == nil {
		var disk diskShape
		if err := json.Unmarshal(data, &disk); err != nil {
			return cfg, prov, fmt.Errorf("parse %s: %w", path, err)
		}
		applyDisk(&cfg, &disk, prov)
	}

	applyEnv(&cfg, prov)
	applyFlags(&cfg, opts, prov)
	if cfg.UI.Theme == "night-ink" {
		cfg.UI.Theme = "lychee-paper"
		warnf(&cfg, "config: retired night-ink theme uses lychee-paper")
	}

	if err := cfg.Validate(); err != nil {
		return cfg, prov, err
	}
	sort.Strings(cfg.Warnings)
	return cfg, prov, nil
}

func warnf(cfg *Config, format string, args ...any) {
	cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(format, args...))
}

func applyDisk(cfg *Config, disk *diskShape, prov Provenance) {
	mark := func(section string) { prov[section] = source{Section: section, From: "file"} }

	if disk.Server != nil {
		var s Server
		if err := json.Unmarshal(*disk.Server, &s); err == nil {
			if s.Host != "" {
				cfg.Server.Host = s.Host
			}
			if s.Port != 0 {
				cfg.Server.Port = s.Port
			}
			mark("server")
		} else {
			warnf(cfg, "config: server section ignored: %v", err)
		}
	}
	if disk.Workspace != nil {
		var w Workspace
		if err := json.Unmarshal(*disk.Workspace, &w); err == nil && strings.TrimSpace(w.Root) != "" {
			cfg.Workspace.Root = strings.TrimSpace(w.Root)
			mark("workspace")
		}
	}
	if disk.AI != nil {
		var ai AI
		if err := json.Unmarshal(*disk.AI, &ai); err == nil {
			mergeAI(cfg, ai)
			mark("ai")
		} else {
			warnf(cfg, "config: ai section ignored: %v", err)
		}
	}
	if disk.UI != nil {
		var ui UI
		if err := json.Unmarshal(*disk.UI, &ui); err == nil && ui.Theme != "" {
			cfg.UI.Theme = ui.Theme
			mark("ui")
		}
	}

	// Legacy flat keys fill only unset canonical fields.
	if disk.AskAiProviders != nil && disk.AI == nil {
		var legacy legacyAI
		if err := json.Unmarshal(*disk.AskAiProviders, &legacy); err == nil {
			ai := AI{DefaultProvider: legacy.Default, SearchEngine: legacy.SearchEngine, Bindings: legacy.Bindings}
			for _, p := range legacy.Providers {
				ai.Providers = append(ai.Providers, Provider{ID: p.ID, Kind: p.Kind, Name: p.Name, BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.Model, ContextWindowTokens: p.ContextWindowTokens, Reasoning: p.Reasoning, Thinking: p.Thinking})
			}
			mergeAI(cfg, ai)
			mark("ai")
		}
	} else if disk.AskAiProviders != nil && disk.AI != nil {
		warnf(cfg, "config: both 'ai' and legacy 'askAiProviders' present; sectioned 'ai' wins")
	}
}

func mergeAI(cfg *Config, ai AI) {
	if ai.DefaultProvider != "" {
		cfg.AI.DefaultProvider = ai.DefaultProvider
	}
	if ai.SearchEngine != "" {
		cfg.AI.SearchEngine = ai.SearchEngine
	}
	if ai.Providers != nil {
		cfg.AI.Providers = ai.Providers
	}
	if ai.Bindings != nil {
		cfg.AI.Bindings = ai.Bindings
	}
}

// applyEnv applies LWA_* environment overrides and reads legacy names.
func applyEnv(cfg *Config, prov Provenance) {
	mark := func(section string) { prov[section] = source{Section: section, From: "env"} }

	if v := strings.TrimSpace(envname.Value("LWA_SERVER_PORT")); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = port
			mark("server")
		} else {
			warnf(cfg, "config: LWA_SERVER_PORT=%q is not a number; ignored", v)
		}
	}
	if v := strings.TrimSpace(envname.Value("LWA_WORKSPACE_ROOT")); v != "" {
		cfg.Workspace.Root = v
		mark("workspace")
	} else if v := strings.TrimSpace(os.Getenv("WORKSPACE")); v != "" {
		cfg.Workspace.Root = v
		mark("workspace")
	}
}

// applyFlags applies explicit CLI overrides last.
func applyFlags(cfg *Config, opts Options, prov Provenance) {
	if opts.WorkspaceRoot != "" {
		cfg.Workspace.Root = opts.WorkspaceRoot
		prov["workspace"] = source{Section: "workspace", From: "flag"}
	}
	if opts.Port != 0 {
		cfg.Server.Port = opts.Port
		prov["server"] = source{Section: "server", From: "flag"}
	}
}

// ProjectsRoot derives the projects directory from the resolved workspace.
func (c *Config) ProjectsRoot() string {
	root := c.WorkspaceRootResolved()
	return filepath.Join(root, "projects")
}

// WorkspaceRootResolved returns the effective workspace root: the resolved
// value or the discovered repository root.
func (c *Config) WorkspaceRootResolved() string {
	if c.Workspace.Root != "" {
		return c.Workspace.Root
	}
	return workspaceBase()
}

// OpenRuntime selects and locks the data root before reading authoritative configuration.
func OpenRuntime(opts Options, defaultRoot string, validate func(string, *paths.Metadata) error) (Config, *paths.Lease, error) {
	override := opts.WorkspaceRoot != "" || envname.Value("LWA_WORKSPACE_ROOT") != "" || os.Getenv("WORKSPACE") != ""
	if opts.WorkspaceRoot == "" {
		opts.WorkspaceRoot = envname.Value("LWA_WORKSPACE_ROOT")
	}
	if opts.WorkspaceRoot == "" {
		opts.WorkspaceRoot = os.Getenv("WORKSPACE")
	}
	lease, e := paths.OpenWorkspace(override, func() (string, error) {
		legacy := opts
		base := defaultRoot
		if legacy.WorkspaceRoot != "" {
			base = legacy.WorkspaceRoot
		}
		if legacy.Path == "" {
			legacy.Path = filepath.Join(base, "config.local.json")
		}
		cfg, prov, e := Load(legacy)
		if e != nil {
			return "", e
		}
		if !override && prov["workspace"].From == "default" {
			return base, nil
		}
		selected := cfg.WorkspaceRootResolved()
		if !override && base != selected {
			for _, name := range []string{"projects", "preferences.md", "treasures"} {
				path := filepath.Join(base, name)
				if info, err := os.Stat(path); err == nil {
					if info.IsDir() {
						items, err := os.ReadDir(path)
						if err != nil {
							return "", err
						}
						if len(items) == 0 {
							continue
						}
					}
					return "", fmt.Errorf("legacy workspace locations conflict: %s and %s", base, selected)
				} else if !os.IsNotExist(err) {
					return "", err
				}
			}
			if _, err := os.Stat(selected); err != nil {
				return "", fmt.Errorf("legacy workspace unavailable: %w", err)
			}
		}
		return selected, nil
	}, validate)
	if e != nil {
		return Config{}, nil, e
	}
	opts.WorkspaceRoot = lease.Root
	if opts.Path == "" {
		opts.Path = filepath.Join(lease.Root, "config.local.json")
	}
	cfg, _, e := Load(opts)
	if e == nil {
		e = paths.ConfigureWorkspace(lease.Root)
	}
	if e != nil {
		lease.Close()
		return Config{}, nil, e
	}
	return cfg, lease, nil
}
