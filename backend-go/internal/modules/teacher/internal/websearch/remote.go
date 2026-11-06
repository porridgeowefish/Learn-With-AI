package websearch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type remoteSearch struct{ provider, key string }

func (s remoteSearch) Search(ctx context.Context, query string) ([]Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	if s.provider == "zhipu-plan" {
		return s.plan(ctx, query)
	}
	endpoint := "https://api.deepseek.com/v1/tools/search"
	body := map[string]any{"query": query, "count": 5}
	if s.provider == "yuanbao" {
		endpoint = "https://api.wsa.cloud.tencent.com/SearchPro"
		body = map[string]any{"Query": query}
	}
	raw, _, err := s.post(ctx, endpoint, body, "")
	if err != nil {
		return nil, err
	}
	var data struct {
		Results []struct {
			Title, URL, Summary, Snippet string
			Site                         string `json:"site_name"`
		}
		Error    *struct{ Message string }
		Response struct {
			Pages []string
			Error *struct{ Code, Message string }
		}
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data.Error != nil {
		return nil, fmt.Errorf("%s: %s", s.provider, data.Error.Message)
	}
	if data.Response.Error != nil {
		return nil, fmt.Errorf("元宝搜索 %s: %s", data.Response.Error.Code, data.Response.Error.Message)
	}
	out := []Result{}
	for _, r := range data.Results {
		snippet := r.Summary
		if snippet == "" {
			snippet = r.Snippet
		}
		out = append(out, Result{Title: r.Title, URL: r.URL, Snippet: snippet, Media: r.Site})
	}
	for _, page := range data.Response.Pages {
		var r struct{ Title, URL, Passage, Content, Site string }
		if err = json.Unmarshal([]byte(page), &r); err != nil {
			return nil, fmt.Errorf("元宝搜索结果格式无效: %w", err)
		}
		if r.Passage == "" {
			r.Passage = r.Content
		}
		out = append(out, Result{Title: r.Title, URL: r.URL, Snippet: r.Passage, Media: r.Site})
	}
	return out, nil
}

func (s remoteSearch) post(ctx context.Context, endpoint string, body any, session string) ([]byte, string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	req.Header.Set("MCP-Protocol-Version", "2024-11-05")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("%s HTTP %d: %s", s.provider, resp.StatusCode, strings.ReplaceAll(string(raw), s.key, "[redacted]"))
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(strings.NewReader(string(raw)))
		scanner.Buffer(make([]byte, 4096), 2<<20)
		var event strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				event.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			} else if line == "" && event.Len() > 0 {
				var envelope map[string]json.RawMessage
				if json.Unmarshal([]byte(event.String()), &envelope) == nil && envelope["id"] != nil {
					return []byte(event.String()), resp.Header.Get("Mcp-Session-Id"), nil
				}
				event.Reset()
			}
		}
		if event.Len() > 0 {
			raw = []byte(event.String())
		} else {
			return nil, "", fmt.Errorf("搜索服务未返回MCP响应")
		}
	}
	return raw, resp.Header.Get("Mcp-Session-Id"), nil
}

func (s remoteSearch) plan(ctx context.Context, query string) ([]Result, error) {
	endpoint := zhipuBaseURL + "/api/mcp/web_search_prime/mcp"
	rpc := func(id int, method string, params any) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	}
	raw, session, err := s.post(ctx, endpoint, rpc(1, "initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "LLL", "version": "1.0"}}), "")
	if err != nil {
		return nil, err
	}
	var init struct{ Error *struct{ Message string } }
	if err = json.Unmarshal(raw, &init); err != nil {
		return nil, err
	}
	if init.Error != nil {
		return nil, fmt.Errorf("智谱Plan初始化失败: %s", init.Error.Message)
	}
	_, _, err = s.post(ctx, endpoint, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, session)
	if err != nil {
		return nil, err
	}
	raw, _, err = s.post(ctx, endpoint, rpc(2, "tools/call", map[string]any{"name": "webSearchPrime", "arguments": map[string]any{"search_query": query}}), session)
	if err != nil {
		return nil, err
	}
	var response struct {
		Error  *struct{ Message string }
		Result struct {
			IsError bool
			Content []struct{ Type, Text string }
		}
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, fmt.Errorf("智谱Plan搜索: %s", response.Error.Message)
	}
	var text strings.Builder
	for _, content := range response.Result.Content {
		if content.Type == "text" {
			text.WriteString(content.Text)
			text.WriteByte('\n')
		}
	}
	if response.Result.IsError {
		return nil, fmt.Errorf("智谱Plan搜索: %s", strings.ReplaceAll(text.String(), s.key, "[redacted]"))
	}
	if text.Len() == 0 {
		return []Result{}, nil
	}
	return []Result{{Title: "智谱 Plan 搜索结果", Snippet: text.String()}}, nil
}
