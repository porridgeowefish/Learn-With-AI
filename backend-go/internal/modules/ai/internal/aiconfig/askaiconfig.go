// Package askaiconfig loads/saves the Ask-AI provider config in config.local.json.
package askaiconfig

import (
	"context"
	"fmt"
	"strings"
	"time"

	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

const ManagedKind = "lwa-cloud"
const ManagedPrefix = ManagedKind + ":"
const legacyManagedPrefix = "lll-cloud:"

func IsManagedProvider(id string) bool {
	return strings.HasPrefix(id, ManagedPrefix) || strings.HasPrefix(id, legacyManagedPrefix)
}

func canonicalProviderID(id string) string {
	if model, ok := strings.CutPrefix(id, legacyManagedPrefix); ok {
		return ManagedPrefix + model
	}
	return id
}

// Provider is one configured Ask-AI model source.
type Provider struct {
	ManagedMaxInput     int    `json:"-"`
	ManagedMaxOutput    int    `json:"-"`
	MaxConcurrency      int    `json:"maxConcurrency,omitempty"`
	ID                  string `json:"id"`
	Kind                string `json:"kind"` // "openai" | "anthropic"
	Name                string `json:"name"`
	BaseURL             string `json:"baseURL"`
	APIKey              string `json:"apiKey"`
	APIKeyEnv           string `json:"apiKeyEnv,omitempty"`
	Model               string `json:"model"`
	ContextWindowTokens int    `json:"contextWindowTokens,omitempty"`
	Reasoning           bool   `json:"reasoning,omitempty"` // openai-compatible: disclosed summary only
	Thinking            bool   `json:"thinking,omitempty"`  // provider capability; raw thinking is never exposed
}

// Config is the askAiProviders section.
type Config struct {
	managed               *Provider
	cloud                 *llmcloud.Client
	ForegroundConcurrency int                `json:"foregroundConcurrency,omitempty"`
	BackgroundConcurrency int                `json:"backgroundConcurrency,omitempty"`
	Default               string             `json:"default"`
	SearchEngine          string             `json:"searchEngine"` // "google" | "bing"
	Providers             []Provider         `json:"providers"`
	Bindings              map[string]Binding `json:"bindings,omitempty"`
}

type Binding struct {
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
}

// Enabled reports whether at least one provider with key+baseURL+model exists.
func (c *Config) Enabled() bool {
	if c == nil {
		return false
	}
	if c.managed != nil {
		return true
	}
	for _, p := range c.Providers {
		if (p.Kind == "siwc" || (p.APIKey != "" && p.BaseURL != "")) && p.Model != "" {
			return true
		}
	}
	return false
}

// Find returns the provider with id, else the default, else the first, else nil.
func (c *Config) FindExact(id string) *Provider {
	if c == nil || id == "" {
		return nil
	}
	if c.managed != nil && canonicalProviderID(id) == c.managed.ID {
		return c.managed
	}
	if IsManagedProvider(id) {
		if c.cloud == nil {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		return managedProvider(c.cloud.ModelForConversation(ctx, strings.TrimPrefix(canonicalProviderID(id), ManagedPrefix)))
	}
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			return &c.Providers[i]
		}
	}
	return nil
}

func (c *Config) Find(id string) *Provider {
	if c == nil {
		return nil
	}
	if IsManagedProvider(id) {
		return c.FindExact(id)
	}
	if id != "" {
		for i := range c.Providers {
			if c.Providers[i].ID == id {
				return &c.Providers[i]
			}
		}
	}
	for i := range c.Providers {
		if c.Providers[i].ID == c.Default {
			return &c.Providers[i]
		}
	}
	if len(c.Providers) > 0 {
		return &c.Providers[0]
	}
	return nil
}

// Resolve returns a copy of the provider selected for a logical AI service.
// Legacy configurations without bindings continue to use Find("").
func (c *Config) Resolve(service string) *Provider {
	if c == nil {
		return nil
	}
	if managed := c.Managed(service); managed != nil {
		return managed
	}
	return c.ResolveDirect(service)
}

// ResolveDirect selects only local or user-configured connections.
func (c *Config) ResolveDirect(service string) *Provider {
	if c == nil {
		return nil
	}
	binding, ok := c.Bindings[service]
	if IsManagedProvider(binding.ProviderID) {
		return nil
	}
	provider := c.Find(binding.ProviderID)
	if !ok {
		provider = c.Find("")
	}
	if provider == nil {
		return nil
	}
	resolved := *provider
	if ok && strings.TrimSpace(binding.Model) != "" {
		resolved.Model = strings.TrimSpace(binding.Model)
	}
	return &resolved
}

func (c *Config) Managed(service string) *Provider {
	switch service {
	case "teacher", "review", "practice", "disciplineMap", "section-summary", "notes":
		return c.managed
	default:
		return nil
	}
}

func (p Provider) HasConnection() bool {
	return p.Kind == ManagedKind || p.Kind == "siwc" || (p.APIKey != "" && p.BaseURL != "")
}

// UseConfigPathForTest redirects the config file path and returns a restore
// function. For tests in OTHER packages (e.g. server handler tests) that
// need to isolate section-preserving settings writes.
func UseConfigPathForTest(path string) (restore func()) {
	return platformconfig.UsePathForTest(path)
}

func managedProvider(model llmcloud.Model) *Provider {
	if model.ID == "" {
		return nil
	}
	return &Provider{ID: ManagedPrefix + model.ID, Kind: ManagedKind, Name: model.Name, Model: model.ID,
		ContextWindowTokens: model.MaxInput + model.MaxOutput, ManagedMaxInput: model.MaxInput, ManagedMaxOutput: model.MaxOutput}
}

// Load reads local provider settings and the current cloud session without making cloud requests.
func Load() (*Config, error) {
	settings, ok, err := platformconfig.LoadAI()
	if err != nil {
		return nil, err
	}
	var managed *Provider
	var cloudSession *llmcloud.Client
	if client, err := llmcloud.Default(); err == nil {
		if _, active := client.Current(); active {
			cloudSession = client
			managed = managedProvider(client.Selected())
		}
	}
	if !ok && cloudSession == nil {
		return nil, nil
	}
	cfg := Config{ForegroundConcurrency: settings.ForegroundConcurrency, BackgroundConcurrency: settings.BackgroundConcurrency, Default: settings.Default, SearchEngine: settings.SearchEngine, Bindings: make(map[string]Binding, len(settings.Bindings))}
	cfg.managed = managed
	cfg.cloud = cloudSession
	for name, binding := range settings.Bindings {
		cfg.Bindings[name] = Binding{ProviderID: binding.ProviderID, Model: binding.Model}
	}
	for _, provider := range settings.Providers {
		cfg.Providers = append(cfg.Providers, Provider{MaxConcurrency: provider.MaxConcurrency, ID: provider.ID, Kind: provider.Kind, Name: provider.Name, BaseURL: provider.BaseURL, APIKey: provider.ResolveAPIKey(), APIKeyEnv: provider.APIKeyEnv, Model: provider.Model, ContextWindowTokens: provider.ContextWindowTokens, Reasoning: provider.Reasoning, Thinking: provider.Thinking})
	}
	cfg.Default = strings.TrimSpace(cfg.Default)
	cfg.SearchEngine = strings.TrimSpace(cfg.SearchEngine)
	for i := range cfg.Providers {
		cfg.Providers[i].ID = strings.TrimSpace(cfg.Providers[i].ID)
		cfg.Providers[i].BaseURL = strings.TrimSpace(cfg.Providers[i].BaseURL)
		cfg.Providers[i].APIKey = strings.TrimSpace(cfg.Providers[i].APIKey)
		cfg.Providers[i].Model = strings.TrimSpace(cfg.Providers[i].Model)
		if cfg.Providers[i].ContextWindowTokens < 0 {
			cfg.Providers[i].ContextWindowTokens = 0
		}
	}
	return &cfg, nil
}

// Save writes the askAiProviders section back to config.local.json, preserving
// all other top-level keys via a raw-JSON round-trip. Atomic write.
func Save(cfg Config) error {
	if cfg.ForegroundConcurrency < 0 || cfg.ForegroundConcurrency > 32 || cfg.BackgroundConcurrency < 0 || cfg.BackgroundConcurrency > 32 {
		return fmt.Errorf("invalid concurrency limits")
	}
	for _, p := range cfg.Providers {
		if p.MaxConcurrency < 0 || p.MaxConcurrency > 64 {
			return fmt.Errorf("invalid provider concurrency limit")
		}
	}
	old, _, err := platformconfig.LoadAI()
	if err != nil {
		return err
	}
	oldByID := map[string]platformconfig.Provider{}
	for _, p := range old.Providers {
		oldByID[p.ID] = p
	}
	settings := platformconfig.AISettings{ForegroundConcurrency: cfg.ForegroundConcurrency, BackgroundConcurrency: cfg.BackgroundConcurrency, Default: cfg.Default, SearchEngine: cfg.SearchEngine, Bindings: make(map[string]platformconfig.Binding, len(cfg.Bindings))}
	for name, binding := range cfg.Bindings {
		settings.Bindings[name] = platformconfig.Binding{ProviderID: binding.ProviderID, Model: binding.Model}
	}
	for _, provider := range cfg.Providers {
		previous, ok := oldByID[provider.ID]
		if ok && provider.APIKey == previous.ResolveAPIKey() && previous.APIKeyEnv != "" && provider.APIKeyEnv == previous.APIKeyEnv {
			provider.APIKey = previous.APIKey
		}
		settings.Providers = append(settings.Providers, platformconfig.Provider{MaxConcurrency: provider.MaxConcurrency, ID: provider.ID, Kind: provider.Kind, Name: provider.Name, BaseURL: provider.BaseURL, APIKey: provider.APIKey, APIKeyEnv: provider.APIKeyEnv, Model: provider.Model, ContextWindowTokens: provider.ContextWindowTokens, Reasoning: provider.Reasoning, Thinking: provider.Thinking})
	}
	return platformconfig.SaveAI(settings)
}
