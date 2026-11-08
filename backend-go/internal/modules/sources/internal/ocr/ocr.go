// Package ocr turns an uploaded image source into citable markdown text via
// a configured vision model (askAiProviders.bindings.ocr). It replaces the
// CLI-agent parse path for image files only when that binding resolves;
// otherwise uploads keep the iteration-16 behavior.
package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
	impl "github.com/xmz14/lll/backend-go/internal/modules/sources/internal/store"
	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
)

const maxImageBytes = 8 << 20

// visionProvider is the minimal provider shape the OCR call needs; it is
// satisfied by resolved askAiProviders entries.
type visionProvider struct {
	Kind    string
	BaseURL string
	APIKey  string
	Model   string
}

// Configured reports whether the ocr binding resolves to a usable provider.
func Configured() bool {
	return resolveProvider() != nil
}

// resolveProvider reads askAiProviders.bindings.ocr through the shared config
// layer. Unlike the teacher binding, OCR requires an explicit binding; there
// is no default fallback.
func resolveProvider() *visionProvider {
	settings, ok, err := platformconfig.LoadAI()
	if err != nil || !ok {
		return nil
	}
	binding, has := settings.Bindings["ocr"]
	if !has {
		return nil
	}
	var provider *platformconfig.Provider
	for i := range settings.Providers {
		if settings.Providers[i].ID == binding.ProviderID {
			provider = &settings.Providers[i]
			break
		}
	}
	if provider == nil {
		return nil
	}
	resolved := visionProvider{Kind: provider.Kind, BaseURL: strings.TrimSpace(provider.BaseURL), APIKey: strings.TrimSpace(provider.APIKey), Model: strings.TrimSpace(provider.Model)}
	if resolved.APIKey == "" && provider.APIKeyEnv != "" {
		resolved.APIKey = strings.TrimSpace(os.Getenv(provider.APIKeyEnv))
	}
	if binding.Model != "" {
		resolved.Model = strings.TrimSpace(binding.Model)
	}
	if resolved.Kind == "" || resolved.BaseURL == "" || resolved.APIKey == "" || resolved.Model == "" {
		return nil
	}
	return &resolved
}

// ProcessImageOCR reads the revision's original image, extracts text through
// the vision model, commits derived/content.md, and flips the source to
// ready. The caller decides status transitions for failures.
func ProcessImageOCR(slug string, store *impl.Store, sourceID, revisionID string) error {
	_, revision, err := store.Get(sourceID)
	if err != nil || revision.RevisionID != revisionID {
		if err != nil {
			return err
		}
		return errors.New("revision mismatch")
	}
	root, err := workspace.ProjectRootForSlug(slug)
	if err != nil {
		return err
	}
	imagePath := filepath.Join(root, "sources", sourceID, "revisions", revisionID, filepath.FromSlash(revision.Original.Path))
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return err
	}
	if len(data) > maxImageBytes {
		return errors.New("image exceeds OCR size limit")
	}
	mediaType := revision.Original.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	provider := resolveProvider()
	if provider == nil {
		return errors.New("ocr provider not configured")
	}
	markdown, err := extract(context.Background(), *provider, mediaType, data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(markdown) == "" {
		return errors.New("ocr produced no text")
	}
	if _, err := store.CommitDerived(sourceID, revisionID, map[string][]byte{"content.md": []byte(markdown)}, map[string]string{"content.md": "text/markdown; charset=utf-8"}); err != nil {
		return err
	}
	_, err = store.SetStatus(sourceID, "ready", "", "")
	return err
}

func extract(ctx context.Context, provider visionProvider, mediaType string, image []byte) (string, error) {
	extractionPrompt, promptErr := ai.RenderPrompt("auxiliary/ocr", nil)
	if promptErr != nil {
		return "", promptErr
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	encoded := base64.StdEncoding.EncodeToString(image)
	var url string
	var payload []byte
	var requestErr error
	if provider.Kind == "anthropic" {
		url = strings.TrimRight(provider.BaseURL, "/") + "/v1/messages"
		body := map[string]any{
			"model":      provider.Model,
			"max_tokens": platformconfig.ModelOutputTokens,
			"messages": []map[string]any{{"role": "user", "content": []map[string]any{
				{"type": "image", "source": map[string]any{"type": "base64", "media_type": mediaType, "data": encoded}},
				{"type": "text", "text": extractionPrompt},
			}}},
		}
		payload, requestErr = json.Marshal(body)
	} else {
		url = strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"
		body := map[string]any{
			"model": provider.Model,
			platformconfig.ModelOutputLimitKey(provider.BaseURL, provider.Model): platformconfig.ModelOutputTokens,
			"messages": []map[string]any{{"role": "user", "content": []map[string]any{
				{"type": "text", "text": extractionPrompt},
				{"type": "image_url", "image_url": map[string]any{"url": "data:" + mediaType + ";base64," + encoded}},
			}}},
		}
		payload, requestErr = json.Marshal(body)
	}
	if requestErr != nil {
		return "", requestErr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.Kind == "anthropic" {
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ocr status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return parseVisionText(provider.Kind, raw)
}

// parseVisionText pulls the assistant text out of one non-streaming vision
// response for either provider kind. Field paths follow each API's spec.
func parseVisionText(kind string, raw []byte) (string, error) {
	if kind == "anthropic" {
		var payload struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return "", err
		}
		if payload.Error != nil {
			return "", errors.New("ocr: " + payload.Error.Message)
		}
		var out strings.Builder
		for _, block := range payload.Content {
			if block.Type == "text" {
				out.WriteString(block.Text)
			}
		}
		return out.String(), nil
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	if payload.Error != nil {
		return "", errors.New("ocr: " + payload.Error.Message)
	}
	if len(payload.Choices) == 0 {
		return "", errors.New("ocr: empty response")
	}
	return payload.Choices[0].Message.Content, nil
}
