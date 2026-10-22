package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	config "github.com/xmz14/lll/backend-go/internal/platform/config"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type PageProvider struct{ ID, Kind, BaseURL, APIKey, Model string }

func LocalPageAvailable() bool {
	_, available := localPageProvider()
	return available
}

// Capture the selected purpose before queueing. Do not run the potentially
// slow system probe here, and never let a later cloud binding adopt this task.
func AutomaticPageSelection() (providerID, model string) {
	settings, err := ai.LoadConfig()
	if err == nil && settings != nil {
		binding, exists := settings.Bindings["documentStructure"]
		if !exists {
			binding = settings.Bindings["ocr"]
		}
		if binding.ProviderID != "" {
			model = binding.Model
			if model == "" {
				for _, provider := range settings.Providers {
					if provider.ID == binding.ProviderID {
						model = provider.Model
						break
					}
				}
			}
			return binding.ProviderID, model
		}
	}
	return localIdentity()
}

// These GLM flagship IDs accept text only. Unknown/custom routes remain
// provider-controlled; do not guess capabilities from arbitrary model names.
func knownTextOnlyModel(baseURL, model string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "open.bigmodel.cn", "api.z.ai", "tokenhub.tencentmaas.com", "tokenhub-intl.tencentcloudmaas.com":
	default:
		return false
	}
	switch strings.ToLower(model) {
	case "glm-5", "glm-5.1", "glm-5.2", "glm-5.3", "glm-5-turbo":
		return true
	}
	return false
}

type PageResult struct {
	Content string
	Tokens  map[string]int
	Known   bool
}

func normalizePageUsage(raw map[string]int) (map[string]int, bool) {
	tokens := map[string]int{}
	known := false
	for _, field := range []struct{ native, canonical string }{
		{"prompt_tokens", "inputTokens"}, {"completion_tokens", "outputTokens"},
		{"input_tokens", "inputTokens"}, {"output_tokens", "outputTokens"},
	} {
		if value, exists := raw[field.native]; exists && value >= 0 {
			tokens[field.canonical] = value
			known = true
		}
	}
	return tokens, known
}

func ResolvePageProvider(providerID, model string) (PageProvider, error) {
	if providerID == "windows-local-ocr" || providerID == "macos-local-ocr" || providerID == "linux-local-ocr" || providerID == "system-local-ocr" {
		if local, ok := localPageProvider(); ok && local.ID == providerID {
			return local, nil
		}
		if providerID == "windows-local-ocr" {
			return PageProvider{}, errors.New("windows-ocr-unavailable")
		}
		return PageProvider{}, errors.New("local-ocr-unavailable")
	}
	settings, e := ai.LoadConfig()
	if e != nil || settings == nil {
		if providerID == "" {
			if local, available := localPageProvider(); available {
				return local, nil
			}
		}
		return PageProvider{}, errors.New("vision-binding-required")
	}
	binding, has := settings.Bindings["documentStructure"]
	if !has {
		binding, has = settings.Bindings["ocr"]
	}
	if providerID == "" {
		if !has || binding.ProviderID == "" {
			if local, available := localPageProvider(); available {
				return local, nil
			}
			return PageProvider{}, errors.New("vision-binding-required")
		}
		providerID = binding.ProviderID
	}
	for _, p := range settings.Providers {
		if p.ID != providerID {
			continue
		}
		if model == "" {
			if has && binding.ProviderID == p.ID && binding.Model != "" {
				model = binding.Model
			} else {
				model = p.Model
			}
		}
		key := p.APIKey
		if p.APIKeyEnv != "" {
			key = os.Getenv(p.APIKeyEnv)
		}
		if key == "" || p.BaseURL == "" || model == "" || (p.Kind != "openai" && p.Kind != "anthropic") {
			return PageProvider{}, errors.New("provider-unavailable")
		}
		if knownTextOnlyModel(p.BaseURL, model) {
			return PageProvider{}, errors.New("vision-model-unsupported")
		}
		return PageProvider{ID: p.ID, Kind: p.Kind, BaseURL: p.BaseURL, APIKey: key, Model: model}, nil
	}
	return PageProvider{}, errors.New("provider-unavailable")
}
func ExtractPage(ctx context.Context, p PageProvider, image []byte) (PageResult, error) {
	if len(image) > 8<<20 {
		return PageResult{}, errors.New("image-limit")
	}
	if IsLocal(p) {
		return extractLocalPage(ctx, image)
	}
	ctx, c := context.WithTimeout(ctx, 120*time.Second)
	defer c()
	encoded := base64.StdEncoding.EncodeToString(image)
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	payload := map[string]any{"model": p.Model, "max_tokens": config.ModelOutputTokens, "messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": extractionPrompt}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + encoded}}}}}}
	if p.Kind == "anthropic" {
		url = strings.TrimRight(p.BaseURL, "/") + "/v1/messages"
		payload["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": "image/png", "data": encoded}}, map[string]string{"type": "text", "text": extractionPrompt}}}}
	}
	if p.Kind != "anthropic" {
		delete(payload, "max_tokens")
		payload[config.ModelOutputLimitKey(p.BaseURL, p.Model)] = config.ModelOutputTokens
	}
	body, e := json.Marshal(payload)
	if e != nil {
		return PageResult{}, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if e != nil {
		return PageResult{}, e
	}
	req.Header.Set("Content-Type", "application/json")
	if p.Kind == "anthropic" {
		req.Header.Set("x-api-key", p.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, e := client.Do(req)
	if e != nil {
		return PageResult{}, errors.New("vision-request-failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PageResult{}, errors.New("vision-provider-error")
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if e != nil {
		return PageResult{}, e
	}
	if len(raw) > 4<<20 {
		return PageResult{}, errors.New("vision-output-limit")
	}
	var meta struct {
		StopReason string `json:"stop_reason"`
		Choices    []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage map[string]int `json:"usage"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return PageResult{}, errors.New("vision-invalid-response")
	}
	tokens, known := normalizePageUsage(meta.Usage)
	if meta.StopReason == "max_tokens" || len(meta.Choices) > 0 && meta.Choices[0].FinishReason == "length" {
		return PageResult{Tokens: tokens, Known: known}, errors.New("vision-output-truncated")
	}
	text, e := parseVisionText(p.Kind, raw)
	if e != nil || strings.TrimSpace(text) == "" {
		return PageResult{Tokens: tokens, Known: known}, errors.New("vision-no-text")
	}
	return PageResult{Content: text, Tokens: tokens, Known: known}, nil
}
