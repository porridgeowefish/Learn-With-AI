// Package askaiconfig loads/saves the Ask-AI provider config in config.local.json.
package askaiconfig

import (
	"strings"

	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
)

// Provider is one configured Ask-AI model source.
type Provider struct {
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
	Default      string             `json:"default"`
	SearchEngine string             `json:"searchEngine"` // "google" | "bing"
	Providers    []Provider         `json:"providers"`
	Bindings     map[string]Binding `json:"bindings,omitempty"`
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
	for _, p := range c.Providers {
		if p.APIKey != "" && p.BaseURL != "" && p.Model != "" {
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
	binding, ok := c.Bindings[service]
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

// UseConfigPathForTest redirects the config file path and returns a restore
// function. For tests in OTHER packages (e.g. server handler tests) that
// need to isolate section-preserving settings writes.
func UseConfigPathForTest(path string) (restore func()) {
	return platformconfig.UsePathForTest(path)
}

// Load reads the askAiProviders section. Returns (nil, nil) when the file or
// section is absent (feature disabled). Returns an error only on parse failure.
func Load() (*Config, error) {
	settings, ok, err := platformconfig.LoadAI()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	cfg := Config{Default: settings.Default, SearchEngine: settings.SearchEngine, Bindings: make(map[string]Binding, len(settings.Bindings))}
	for name, binding := range settings.Bindings {
		cfg.Bindings[name] = Binding{ProviderID: binding.ProviderID, Model: binding.Model}
	}
	for _, provider := range settings.Providers {
		cfg.Providers = append(cfg.Providers, Provider{ID: provider.ID, Kind: provider.Kind, Name: provider.Name, BaseURL: provider.BaseURL, APIKey: provider.ResolveAPIKey(), APIKeyEnv: provider.APIKeyEnv, Model: provider.Model, ContextWindowTokens: provider.ContextWindowTokens, Reasoning: provider.Reasoning, Thinking: provider.Thinking})
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
	old, _, err := platformconfig.LoadAI()
	if err != nil {
		return err
	}
	oldByID := map[string]platformconfig.Provider{}
	for _, p := range old.Providers {
		oldByID[p.ID] = p
	}
	settings := platformconfig.AISettings{Default: cfg.Default, SearchEngine: cfg.SearchEngine, Bindings: make(map[string]platformconfig.Binding, len(cfg.Bindings))}
	for name, binding := range cfg.Bindings {
		settings.Bindings[name] = platformconfig.Binding{ProviderID: binding.ProviderID, Model: binding.Model}
	}
	for _, provider := range cfg.Providers {
		previous, ok := oldByID[provider.ID]
		if ok && provider.APIKey == previous.ResolveAPIKey() && previous.APIKeyEnv != "" && provider.APIKeyEnv == previous.APIKeyEnv {
			provider.APIKey = previous.APIKey
		}
		settings.Providers = append(settings.Providers, platformconfig.Provider{ID: provider.ID, Kind: provider.Kind, Name: provider.Name, BaseURL: provider.BaseURL, APIKey: provider.APIKey, APIKeyEnv: provider.APIKeyEnv, Model: provider.Model, ContextWindowTokens: provider.ContextWindowTokens, Reasoning: provider.Reasoning, Thinking: provider.Thinking})
	}
	return platformconfig.SaveAI(settings)
}
