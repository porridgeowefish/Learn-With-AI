package httpserver

import (
	"context"
	"encoding/json"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/platform/siwc"
)

type catalogModel struct {
	ID                  string `json:"id"`
	ContextWindowTokens *int   `json:"contextWindowTokens"`
	MaxOutputTokens     *int   `json:"maxOutputTokens"`
	Vision              *bool  `json:"vision"`
	Tools               *bool  `json:"tools"`
	Thinking            *bool  `json:"thinking"`
}

// This explicit draft operation is local-only and never saves a connection or
// submits a completion. Credentials and provider response bodies are not errors.
func (s *Server) handleModelCatalog(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var in struct {
		ProviderID string             `json:"providerId"`
		Inline     *ai.ProviderConfig `json:"inline"`
	}
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, 400, "invalid connection draft")
		return
	}
	var provider ai.ProviderConfig
	if in.Inline != nil {
		provider = *in.Inline
	} else {
		cfg, err := ai.LoadConfig()
		if err != nil || cfg == nil {
			httpx.Error(w, 400, "connection unavailable")
			return
		}
		found := cfg.FindExact(in.ProviderID)
		if found == nil {
			httpx.Error(w, 400, "connection unavailable")
			return
		}
		provider = *found
	}
	if provider.Kind == "siwc" {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		ids, err := siwc.CurrentModelCatalog(ctx)
		if err != nil {
			httpx.Error(w, 502, "ChatGPT model catalog unavailable")
			return
		}
		models := make([]catalogModel, 0, len(ids))
		for _, id := range ids {
			models = append(models, catalogModel{ID: id})
		}
		httpx.WriteJSON(w, 200, map[string]any{"models": models, "source": "provider", "verifiedAt": time.Now().UTC().Format(time.RFC3339)})
		return
	}
	endpoint, err := url.Parse(strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/"))
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (provider.Kind != "openai" && provider.Kind != "anthropic") {
		httpx.Error(w, 400, "invalid provider URL or protocol")
		return
	}
	if provider.APIKeyEnv != "" {
		if key := strings.TrimSpace(os.Getenv(provider.APIKeyEnv)); key != "" {
			provider.APIKey = key
		}
	}
	if provider.Kind == "anthropic" && !strings.HasSuffix(endpoint.Path, "/v1") {
		endpoint.Path += "/v1"
	}
	endpoint.Path += "/models"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		httpx.Error(w, 400, "invalid provider URL")
		return
	}
	if provider.Kind == "anthropic" {
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		httpx.Error(w, 502, "model catalog unavailable")
		return
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		httpx.Error(w, res.StatusCode, "model catalog credentials or permissions rejected")
		return
	}
	if res.StatusCode == 404 || res.StatusCode == 405 || res.StatusCode == 501 {
		httpx.WriteJSON(w, 200, map[string]any{"models": []catalogModel{}, "source": "preset", "verifiedAt": "2026-10-11", "warnings": []string{"Use the verified preset list; this provider has no model discovery endpoint."}})
		return
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		httpx.Error(w, 502, "model catalog unavailable")
		return
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		httpx.Error(w, 502, "model catalog response exceeds limit")
		return
	}
	var raw struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength *int   `json:"context_length"`
			MaxInput      *int   `json:"max_input_tokens"`
			MaxTokens     *int   `json:"max_tokens"`
		} `json:"data"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		httpx.Error(w, 502, "invalid model catalog response")
		return
	}
	models := make([]catalogModel, 0)
	seen := map[string]bool{}
	for _, item := range raw.Data {
		id := strings.TrimSpace(item.ID)
		lower := strings.ToLower(id)
		if id == "" || len(id) > 256 || strings.ContainsAny(id, "\r\n\x00") || seen[id] || strings.Contains(lower, "embedding") || strings.Contains(lower, "whisper") || strings.Contains(lower, "tts") || strings.Contains(lower, "image-generation") || strings.Contains(lower, "dall-e") {
			continue
		}
		seen[id] = true
		window := item.ContextLength
		if item.MaxInput != nil {
			window = item.MaxInput
		}
		models = append(models, catalogModel{ID: id, ContextWindowTokens: window, MaxOutputTokens: item.MaxTokens})
		if len(models) >= 1000 {
			break
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	httpx.WriteJSON(w, 200, map[string]any{"models": models, "source": "provider", "verifiedAt": time.Now().UTC().Format(time.RFC3339)})
}
