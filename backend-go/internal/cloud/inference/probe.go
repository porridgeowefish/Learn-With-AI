package inference

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
)

type ProbeResult struct {
	Status         string `json:"status"`
	Model          string `json:"model"`
	InputTokens    *int   `json:"inputTokens,omitempty"`
	OutputTokens   *int   `json:"outputTokens,omitempty"`
	CacheBreakdown bool   `json:"-"`
}

// Probe checks the connection's authenticated catalog without starting inference.
func (g *Gateway) Probe(ctx context.Context, connectionID string) (ProbeResult, error) {
	upstream, ok := g.providers[connectionID]
	if !ok {
		return ProbeResult{}, errors.New("unknown upstream connection")
	}
	p := upstream.Provider
	endpoint := strings.TrimRight(p.BaseURL, "/")
	if p.Kind == "anthropic" {
		endpoint += "/v1"
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return ProbeResult{}, err
	}
	if p.Kind == "anthropic" {
		req.Header.Set("x-api-key", p.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	result := ProbeResult{Status: "unavailable", Model: p.Model}
	response, err := g.client.Do(req)
	if err != nil {
		return result, nil
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		result.Status = "auth_failed"
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		result.Status = "catalog_unsupported"
	case http.StatusTooManyRequests:
		result.Status = "rate_limited"
	case http.StatusOK:
		var listing struct {
			Data []struct{ ID string } `json:"data"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&listing); err != nil || listing.Data == nil {
			return result, nil
		}
		result.Status = "model_not_listed"
		for _, item := range listing.Data {
			if item.ID == p.Model {
				result.Status = "verified"
				break
			}
		}
	}
	return result, nil
}

// ProbeUsage runs one bounded synthetic request against exactly this connection.
func (g *Gateway) ProbeUsage(ctx context.Context, connectionID string) (ProbeResult, error) {
	upstream, ok := g.providers[connectionID]
	if !ok {
		return ProbeResult{}, errors.New("unknown upstream connection")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	result := ProbeResult{Status: "unavailable", Model: upstream.Provider.Model}
	seenText := false
	var usage map[string]int
	err := ai.StreamUpstream(ctx, g.client, upstream.Provider, ai.GatewayRequest{
		Messages:        []ai.GatewayMessage{{Role: "user", Content: "Reply with OK."}},
		MaxOutputTokens: probeOutputLimit(upstream, 64),
	}, func(event ai.GatewayEvent) {
		if event.Type == "text-delta" && strings.TrimSpace(event.Delta) != "" {
			seenText = true
		}
		if event.Type == "usage" {
			usage = event.Usage
		}
	})
	input, output := usage["inputTokens"], usage["outputTokens"]
	_, totalsKnown := ChargeTokens(usage, 1, 1, 1, 1)
	if totalsKnown {
		result.InputTokens, result.OutputTokens = &input, &output
		_, haveRead := usage["cacheReadTokens"]
		_, haveMiss := usage["cacheMissTokens"]
		result.CacheBreakdown = haveRead || haveMiss
	}
	switch status := probeStreamError(err); {
	case status != "":
		result.Status = status
	case !seenText:
		result.Status = "text_missing"
	case !totalsKnown || ((upstream.RequireCacheUsage || upstream.Cost.RequiresCacheUsage()) && !result.CacheBreakdown):
		result.Status = "usage_missing"
	default:
		result.Status = "verified"
	}
	return result, nil
}

func probeOutputLimit(upstream Upstream, limit int) int {
	if upstream.ModelMaxOutput > 0 {
		return min(limit, upstream.ModelMaxOutput)
	}
	return limit
}

func probeStreamError(err error) string {
	var rateLimit *ai.UpstreamRateLimit
	switch {
	case errors.Is(err, ai.ErrUpstreamAuthRejected):
		return "auth_failed"
	case errors.As(err, &rateLimit):
		return "rate_limited"
	case err != nil:
		return "unavailable"
	default:
		return ""
	}
}
