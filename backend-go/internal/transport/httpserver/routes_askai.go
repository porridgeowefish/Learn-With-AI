package httpserver

import (
	"context"

	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"net/http"
	"os"

	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
)

func (s *Server) handleGetAskAiSettings(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	cfg, err := ai.LoadConfig()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfg == nil {
		cfg = &ai.Config{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"default":      cfg.Default,
		"searchEngine": cfg.SearchEngine,
		"providers":    cfg.Providers,
		"bindings":     cfg.Bindings,
	})
}

func (s *Server) handlePutAskAiSettings(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var in ai.Config
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	// Preserve service bindings when an older configuration editor omits them.
	old, loadErr := ai.LoadConfig()
	if loadErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "unable to load existing AI settings")
		return
	}
	if old != nil {
		if in.Bindings == nil {
			in.Bindings = old.Bindings
		}
	}
	if err := ai.SaveConfig(in); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleProbeAskAi(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var in struct {
		ProviderID string             `json:"providerId"`
		Inline     *ai.ProviderConfig `json:"inline"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	var pc ai.ProviderConfig
	if in.Inline != nil {
		pc = *in.Inline
	} else {
		cfg, err := ai.LoadConfig()
		if err != nil || cfg == nil {
			httpx.Error(w, http.StatusBadRequest, "ask-ai not configured")
			return
		}
		p := cfg.Find(in.ProviderID)
		if p == nil {
			httpx.Error(w, http.StatusBadRequest, "provider not found")
			return
		}
		pc = *p
	}
	if pc.APIKeyEnv != "" {
		if value := strings.TrimSpace(os.Getenv(pc.APIKeyEnv)); value != "" {
			pc.APIKey = value
		}
	}
	probeCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	prov := ai.Provider{Kind: pc.Kind, BaseURL: pc.BaseURL, APIKey: pc.APIKey, Model: pc.Model, Reasoning: pc.Reasoning, Thinking: pc.Thinking}
	_, err := ai.Complete(probeCtx, prov, "Reply with the single word: ok", []ai.AIMessage{{Role: "user", Content: "ping"}})
	if err != nil {
		message := err.Error()
		if pc.APIKey != "" {
			message = strings.ReplaceAll(message, pc.APIKey, "[redacted]")
		}
		if len(message) > 512 {
			message = string([]rune(message)[:min(256, len([]rune(message)))])
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": message})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
