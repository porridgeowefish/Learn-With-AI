// Package websearch is the teacher's public-web search backend. The initial
// provider is the Zhipu web-search API behind a tiny interface so a future
// swap (Tavily, Bocha, SearXNG) stays local to this package.
package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/learningprofile"
	"io"
	"net/http"
	"strings"
	"time"

	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
)

const defaultZhipuBaseURL = "https://open.bigmodel.cn"

// zhipuBaseURL is a seam for tests; production always uses the default.
var zhipuBaseURL = defaultZhipuBaseURL

func setZhipuBaseURL(url string) func() {
	previous := zhipuBaseURL
	zhipuBaseURL = url
	return func() { zhipuBaseURL = previous }
}

type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Media   string `json:"media,omitempty"`
}

type Searcher interface {
	Search(ctx context.Context, query string) ([]Result, error)
}

// Configured returns the search backend from config.local.json, or nil when
// the webSearch section is absent/disabled (the search_web tool is then not
// registered for the teacher).
func Configured() Searcher {
	profile, err := learningprofile.Read()
	if err != nil || !profile.Settings.Search.Enabled {
		return nil
	}
	return FromSettings(profile.Settings.Search)
}

func FromSettings(settings learningprofile.SearchSettings) Searcher {
	if settings.Provider == "zhipu-plan" {
		if p := learningprofile.PlanProvider(settings.ProviderID); p != nil {
			return remoteSearch{provider: "zhipu-plan", key: p.APIKey}
		}
	}
	if (settings.Provider == "deepseek" || settings.Provider == "yuanbao") && strings.TrimSpace(settings.APIKey) != "" {
		return remoteSearch{provider: settings.Provider, key: settings.APIKey}
	}

	if settings.Provider == "chatgpt" && settings.ProviderID != "" {
		return chatgpt{settings.ProviderID}
	}
	if settings.Provider == "zhipu" && settings.APIKey != "" {
		return zhipu{apiKey: settings.APIKey, engine: "search_std"}
	}
	return nil
}

type chatgpt struct{ providerID string }

func (s chatgpt) Search(ctx context.Context, query string) ([]Result, error) {
	cfg, e := ai.LoadConfig()
	if e != nil {
		return nil, e
	}
	p := cfg.FindExact(s.providerID)
	if p == nil || p.Kind != "siwc" {
		return nil, errors.New("请选择已登录的 ChatGPT 连接")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var out strings.Builder
	prompt, promptErr := ai.RenderPrompt("tools/web-search/system", nil)
	if promptErr != nil {
		return nil, promptErr
	}
	e = (ai.Configured{}).Stream(ctx, ai.GatewayRequest{ProviderID: s.providerID, WebSearch: true, System: prompt, Messages: []ai.GatewayMessage{{Role: "user", Content: query}}}, func(ev ai.GatewayEvent) {
		if ev.Type == "text-delta" && out.Len() < 24000 {
			out.WriteString(ev.Delta)
		}
	})
	if e != nil {
		return nil, e
	}
	return []Result{{Title: "联网检索结果", Snippet: out.String()}}, nil
}

// Retained reader for legacy installations; the explicit new opt-in owns access.
func legacyConfigured() Searcher {
	settings, ok, err := platformconfig.LoadWebSearch()
	if err != nil || !ok || strings.TrimSpace(settings.APIKey) == "" {
		return nil
	}
	if settings.Provider != "" && settings.Provider != "zhipu" {
		return nil
	}
	engine := strings.TrimSpace(settings.Engine)
	if engine == "" {
		engine = "search_std"
	}
	return zhipu{apiKey: strings.TrimSpace(settings.APIKey), engine: engine}
}

type zhipu struct {
	apiKey string
	engine string
}

func (z zhipu) Search(ctx context.Context, query string) ([]Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	body := map[string]any{"search_engine": z.engine, "search_query": query, "count": 5}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, zhipuBaseURL+"/api/paas/v4/web_search", strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+z.apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("web-search status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	var payload struct {
		SearchResult []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Content string `json:"content"`
			Media   string `json:"media"`
		} `json:"search_result"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Error != nil {
		return nil, errors.New("web-search " + payload.Error.Code + ": " + payload.Error.Message)
	}
	results := make([]Result, 0, len(payload.SearchResult))
	for _, item := range payload.SearchResult {
		results = append(results, Result{Title: item.Title, URL: item.Link, Snippet: item.Content, Media: item.Media})
	}
	return results, nil
}

// FormatResults packs results into the compact text block fed back to the
// provider as the search_web tool result.
func FormatResults(results []Result) string {
	var out strings.Builder
	if len(results) == 0 {
		return "没有检索到相关结果。"
	}
	for i, result := range results {
		fmt.Fprintf(&out, "[%d] %s", i+1, result.Title)
		if result.Media != "" {
			out.WriteString("（" + result.Media + "）")
		}
		out.WriteByte('\n')
		if result.URL != "" {
			out.WriteString(result.URL + "\n")
		}
		if result.Snippet != "" {
			out.WriteString(strings.TrimSpace(result.Snippet) + "\n")
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}
