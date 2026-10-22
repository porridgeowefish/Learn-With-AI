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

func openAIBody(p Provider, system string, msgs []Message, stream bool) ([]byte, error) {
	out := make([]Message, 0, len(msgs)+1)
	if system != "" {
		out = append(out, Message{Role: "system", Content: system})
	}
	out = append(out, msgs...)
	body := map[string]any{
		"model":    p.Model,
		"messages": out,
		"stream":   stream,
	}
	body[platformconfig.ModelOutputLimitKey(p.BaseURL, p.Model)] = platformconfig.ModelOutputTokens
	return json.Marshal(body)
}

func streamOpenAI(ctx context.Context, p Provider, system string, msgs []Message, onFrame func(Frame)) error {
	body, err := openAIBody(p, system, msgs, true)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai: status %d: %s", resp.StatusCode, string(b))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			onFrame(Frame{Type: "done"})
			return nil
		}
		if payload == "" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			if p.Reasoning && ch.Delta.ReasoningContent != "" {
				onFrame(Frame{Type: "thinking", Content: ch.Delta.ReasoningContent})
			}
			if ch.Delta.Content != "" {
				onFrame(Frame{Type: "text", Content: ch.Delta.Content})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	onFrame(Frame{Type: "done"})
	return nil
}

func completeOpenAI(ctx context.Context, p Provider, system string, msgs []Message) (string, error) {
	body, err := openAIBody(p, system, msgs, false)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai: status %d: %s", resp.StatusCode, string(b))
	}
	var res struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if len(res.Choices) == 0 {
		return "", nil
	}
	return res.Choices[0].Message.Content, nil
}
