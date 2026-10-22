package askaiprovider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
	"io"
	"net/http"
	"strings"
)

func anthropicBody(p Provider, system string, msgs []Message, stream bool) ([]byte, error) {
	req := map[string]any{
		"model":      p.Model,
		"max_tokens": platformconfig.ModelOutputTokens,
		"stream":     stream,
		"messages":   msgs,
	}
	if system != "" {
		req["system"] = system
	}
	if p.Thinking {
		req["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 1024}
	}
	return json.Marshal(req)
}

func streamAnthropic(ctx context.Context, p Provider, system string, msgs []Message, onFrame func(Frame)) error {
	body, err := anthropicBody(p, system, msgs, true)
	if err != nil {
		return err
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, string(b))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var ev struct {
			Type  string          `json:"type"`
			Delta json.RawMessage `json:"delta"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "content_block_delta":
			var d struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			}
			if json.Unmarshal(ev.Delta, &d) == nil {
				switch d.Type {
				case "thinking_delta":
					if p.Thinking && d.Thinking != "" {
						onFrame(Frame{Type: "thinking", Content: d.Thinking})
					}
				case "text_delta":
					if d.Text != "" {
						onFrame(Frame{Type: "text", Content: d.Text})
					}
				}
			}
		case "message_stop":
			onFrame(Frame{Type: "done"})
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	onFrame(Frame{Type: "done"})
	return nil
}

func completeAnthropic(ctx context.Context, p Provider, system string, msgs []Message) (string, error) {
	// Disable thinking for a concise summary.
	p.Thinking = false
	body, err := anthropicBody(p, system, msgs, false)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, string(b))
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, b := range res.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String(), nil
}
