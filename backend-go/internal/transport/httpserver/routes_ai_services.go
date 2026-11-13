package httpserver

import (
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"net/http"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
)

type aiServiceBinding struct {
	ProviderID string `json:"providerId"`
	Model      string `json:"model"`
}

type aiServiceProvider struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name,omitempty"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey"`
	APIKeyEnv string `json:"apiKeyEnv,omitempty"`
}

type aiServicesResource struct {
	Providers []aiServiceProvider         `json:"providers"`
	Bindings  map[string]aiServiceBinding `json:"bindings"`
}

func (s *Server) handleGetAIServices(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	cfg, err := ai.LoadConfig()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "unable to load AI services")
		return
	}
	resource := aiServicesResource{Bindings: map[string]aiServiceBinding{}}
	if cfg != nil {
		for _, provider := range cfg.Providers {
			kind := provider.Kind
			if kind == "openai" {
				kind = "openai-compatible"
			}
			resource.Providers = append(resource.Providers, aiServiceProvider{ID: provider.ID, Kind: kind, Name: provider.Name, BaseURL: provider.BaseURL, APIKey: provider.APIKey, APIKeyEnv: provider.APIKeyEnv})
		}
		for _, name := range []string{"teacher", "notes", "ocr", "documentStructure", "review", "practice", "disciplineMap"} {
			if binding, ok := cfg.Bindings[name]; ok {
				resource.Bindings[name] = aiServiceBinding{ProviderID: binding.ProviderID, Model: binding.Model}
				continue
			}
			if name == "documentStructure" || name == "ocr" {
				continue
			}
			if provider := cfg.FindExact(cfg.Default); provider != nil {
				resource.Bindings[name] = aiServiceBinding{ProviderID: provider.ID, Model: provider.Model}
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resource)
}

func (s *Server) handlePutAIServices(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var in aiServicesResource
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid AI services body")
		return
	}
	old, loadErr := ai.LoadConfig()
	if loadErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "unable to load existing AI settings")
		return
	}
	oldByID := map[string]ai.ProviderConfig{}
	var config ai.Config
	if old != nil {
		config = *old
		for _, provider := range old.Providers {
			oldByID[provider.ID] = provider
		}
	}
	teacherBinding := in.Bindings["teacher"]
	providers := make([]ai.ProviderConfig, 0, len(in.Providers))
	for _, provider := range in.Providers {
		id := strings.TrimSpace(provider.ID)
		kind := provider.Kind
		if kind == "openai-compatible" {
			kind = "openai"
		}
		saved := oldByID[id]
		if saved.Model == "" {
			for _, binding := range in.Bindings {
				if binding.ProviderID == id && binding.Model != "" {
					saved.Model = strings.TrimSpace(binding.Model)
					break
				}
			}
		}
		saved.ID, saved.Kind, saved.Name = id, kind, provider.Name
		saved.BaseURL = strings.TrimSpace(provider.BaseURL)
		saved.APIKey, saved.APIKeyEnv = strings.TrimSpace(provider.APIKey), provider.APIKeyEnv
		providers = append(providers, saved)
	}
	bindings := map[string]ai.Binding{}
	for name, binding := range in.Bindings {
		bindings[name] = ai.Binding{ProviderID: binding.ProviderID, Model: binding.Model}
	}
	config.Default, config.Providers, config.Bindings = teacherBinding.ProviderID, providers, bindings
	if err := ai.SaveConfig(config); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "unable to save AI services")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleProbeAIServices(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Binding    string             `json:"binding"`
		ProviderID string             `json:"providerId"`
		Inline     *aiServiceProvider `json:"inline"`
		Model      string             `json:"model"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid probe request")
		return
	}
	var provider ai.ProviderConfig
	if in.Inline != nil {
		kind := in.Inline.Kind
		if kind == "openai-compatible" {
			kind = "openai"
		}
		provider = ai.ProviderConfig{ID: in.Inline.ID, Kind: kind, BaseURL: in.Inline.BaseURL, APIKey: in.Inline.APIKey, Model: in.Model}
	} else {
		cfg, err := ai.LoadConfig()
		if err != nil || cfg == nil || cfg.Find(in.ProviderID) == nil {
			httpx.Error(w, http.StatusBadRequest, "provider not found")
			return
		}
		provider = *cfg.Find(in.ProviderID)
		if in.Model != "" {
			provider.Model = in.Model
		}
	}
	started := time.Now()
	prompt, promptErr := ai.RenderPrompt("auxiliary/provider-probe", nil)
	if promptErr != nil {
		httpx.Error(w, 500, promptErr.Error())
		return
	}
	_, err := ai.Complete(r.Context(), ai.Provider{ID: provider.ID, Kind: provider.Kind, BaseURL: provider.BaseURL, APIKey: provider.APIKey, Model: provider.Model}, prompt, []ai.AIMessage{{Role: "user", Content: "ping"}})
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "latencyMs": time.Since(started).Milliseconds(), "error": "provider probe failed"})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "latencyMs": time.Since(started).Milliseconds(), "capabilities": map[string]bool{"streaming": true, "toolUse": true, "reasoningSummary": true}})
}
