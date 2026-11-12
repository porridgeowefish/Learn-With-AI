// Package inference dispatches admitted managed inference requests to providers.
package inference

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/routing"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
)

var (
	ErrUpstream = errors.New("managed inference upstream request failed")
	ErrTools    = errors.New("system model does not support tool requests")
)

type Upstream struct {
	Connection        routing.Connection
	Provider          ai.ProviderConfig
	Cost              CostRate
	ModelMaxOutput    int
	RequireCacheUsage bool
}

// CostRate is denominated in billionths of Currency per token.
type CostRate struct {
	Currency                     string
	Input, Cached, Write, Output int64
}

func (c CostRate) RequiresCacheUsage() bool {
	return c.Currency != "" && c.Input != c.Cached
}

func (c CostRate) charge(usage map[string]int) *int64 {
	if c.Currency == "" {
		return nil
	}
	nanos, known := ChargeTokens(usage, c.Input, c.Cached, c.Write, c.Output)
	if !known {
		return nil
	}
	return &nanos
}

type Attempt struct {
	ConnectionID string
	Status       string
	Usage        map[string]int
	Elapsed      time.Duration
	Currency     string
	CostNanos    *int64
}

type Gateway struct {
	pool      *routing.Pool
	providers map[string]Upstream
	waiting   chan struct{}
}

func RequiresTools(tools []ai.Tool, messages []ai.GatewayMessage) bool {
	if len(tools) != 0 {
		return true
	}
	for _, message := range messages {
		if len(message.ToolCalls) != 0 || message.ToolCallID != "" {
			return true
		}
	}
	return false
}

func (g *Gateway) SupportsTools(model string) bool {
	for _, upstream := range g.providers {
		if upstream.Connection.Model == model && !upstream.Connection.TextOnly {
			return true
		}
	}
	return false
}

// EstimateCost reserves at the highest eligible same-model connection rate.
// A missing rate makes the model ineligible while a global budget is enabled.
func (g *Gateway) EstimateCost(model, currency string, input, output int, needsTools bool) (int64, bool) {
	var highest int64
	for _, upstream := range g.providers {
		if upstream.Connection.Model != model || (needsTools && upstream.Connection.TextOnly) {
			continue
		}
		rate := upstream.Cost
		if rate.Currency != currency || rate.Input <= 0 || rate.Output <= 0 {
			return 0, false
		}
		highest = max(highest, int64(input)*max(rate.Input, rate.Cached, rate.Write)+int64(output)*rate.Output)
	}
	return highest, highest > 0
}

func (g *Gateway) CarryCapacity(previous *Gateway) {
	g.pool.CarryCapacity(previous.pool)
}

type Capacity struct {
	routing.Capacity
	Waiting      int `json:"waiting"`
	WaitingLimit int `json:"waitingLimit"`
}

func (g *Gateway) Capacity() Capacity {
	return Capacity{Capacity: g.pool.Capacity(), Waiting: len(g.waiting), WaitingLimit: cap(g.waiting)}
}

func New(upstreams []Upstream, groups map[string]routing.Limits) (*Gateway, error) {
	connections := make([]routing.Connection, 0, len(upstreams))
	providers := make(map[string]Upstream, len(upstreams))
	for _, upstream := range upstreams {
		p := upstream.Provider
		endpoint, err := url.Parse(p.BaseURL)
		if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
			(endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && endpoint.Hostname() == "127.0.0.1")) ||
			p.Model == "" || p.APIKey == "" || (p.Kind != "openai" && p.Kind != "anthropic") {
			return nil, fmt.Errorf("invalid upstream configuration for connection %q", upstream.Connection.ID)
		}
		for _, rate := range []int64{upstream.Cost.Input, upstream.Cost.Cached, upstream.Cost.Write, upstream.Cost.Output} {
			if rate < 0 || rate > 1_000_000_000 {
				return nil, fmt.Errorf("invalid upstream cost for connection %q", upstream.Connection.ID)
			}
		}
		connections = append(connections, upstream.Connection)
		providers[upstream.Connection.ID] = upstream
	}
	pool, err := routing.NewPool(connections, groups)
	if err != nil {
		return nil, err
	}
	return &Gateway{pool: pool, providers: providers, waiting: make(chan struct{}, 32)}, nil
}

func (g *Gateway) acquire(ctx context.Context, model string, estimatedTokens int, preferred string, needsTools bool, tried []string) (*routing.Lease, error) {
	lease, err := g.pool.Acquire(model, estimatedTokens, preferred, needsTools, tried...)
	if !errors.Is(err, routing.ErrCapacity) || len(tried) > 0 {
		return lease, err
	}
	select {
	case g.waiting <- struct{}{}:
		defer func() { <-g.waiting }()
	default:
		return nil, err
	}
	deadline := time.NewTimer(1500 * time.Millisecond)
	defer deadline.Stop()
	recheck := time.NewTicker(75 * time.Millisecond)
	defer recheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, routing.ErrCapacity
		case <-recheck.C:
			lease, err := g.pool.Acquire(model, estimatedTokens, preferred, needsTools)
			if !errors.Is(err, routing.ErrCapacity) {
				return lease, err
			}
		}
	}
}

// Stream keeps the selected model fixed. The caller supplies an authorized
// request, an estimated input/output budget and a deadline.
func (g *Gateway) Stream(ctx context.Context, model string, estimatedTokens int, preferred string, request ai.GatewayRequest, emit func(ai.GatewayEvent)) ([]Attempt, error) {
	needsTools := RequiresTools(request.Tools, request.Messages)
	if needsTools && !g.SupportsTools(model) {
		return nil, ErrTools
	}
	const maxAttempts = 3
	attempts := make([]Attempt, 0, maxAttempts)
	var tried []string
	var rateLimited, authRejected bool
	for range maxAttempts {
		if err := ctx.Err(); err != nil {
			return attempts, err
		}
		lease, err := g.acquire(ctx, model, estimatedTokens, preferred, needsTools, tried)
		if err != nil {
			if errors.Is(err, routing.ErrCapacity) && authRejected && !rateLimited {
				return attempts, ErrUpstream
			}
			return attempts, err
		}
		id := lease.ConnectionID()
		upstream := g.providers[id]
		var usage map[string]int
		started := time.Now()
		err = ai.StreamUpstream(ctx, upstream.Provider, request, func(event ai.GatewayEvent) {
			if event.Type == "usage" {
				usage = maps.Clone(event.Usage)
			}
			emit(event)
		})
		attempt := Attempt{ConnectionID: id, Elapsed: time.Since(started), Currency: upstream.Cost.Currency,
			Usage: usage, CostNanos: upstream.Cost.charge(usage)}
		var rateLimit *ai.UpstreamRateLimit
		if errors.As(err, &rateLimit) {
			lease.RateLimited(rateLimit.RetryAfter)
			rateLimited = true
			tried = append(tried, id)
			attempt.Status = "rate_limited"
			attempts = append(attempts, attempt)
			continue
		}
		if errors.Is(err, ai.ErrUpstreamAuthRejected) {
			lease.AuthRejected()
			authRejected = true
			tried = append(tried, id)
			attempt.Status = "auth_rejected"
			attempts = append(attempts, attempt)
			continue
		}
		input, inputKnown := usage["inputTokens"]
		output, outputKnown := usage["outputTokens"]
		actual := -1
		if inputKnown && outputKnown && input >= 0 && output >= 0 {
			actual = input + output
		}
		if err != nil {
			lease.Interrupt(actual)
			attempt.Status = "interrupted"
			attempts = append(attempts, attempt)
			if ctx.Err() != nil {
				return attempts, ctx.Err()
			}
			if ai.IsContextOverflow(err) {
				return attempts, err
			}
			return attempts, ErrUpstream
		}
		lease.Finish(actual)
		attempt.Status = "completed"
		attempts = append(attempts, attempt)
		return attempts, nil
	}
	if authRejected && !rateLimited {
		return attempts, ErrUpstream
	}
	return attempts, routing.ErrCapacity
}
