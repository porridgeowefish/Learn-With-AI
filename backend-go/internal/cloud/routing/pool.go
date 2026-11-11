// Package routing controls capacity for the managed inference gateway.
package routing

import (
	"cmp"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"sync"
	"time"
)

var (
	ErrUnknownModel = errors.New("system model has no available connection configuration")
	ErrCapacity     = errors.New("system model has no remaining capacity")
)

const defaultCooldown = 5 * time.Second

type Limits struct {
	Concurrent        int
	RequestsPerMinute int
	TokensPerMinute   int
}

func (l Limits) valid() bool {
	return l.Concurrent > 0 && l.RequestsPerMinute > 0 && l.TokensPerMinute > 0
}

type Connection struct {
	ID, Model, QuotaGroup string
	Priority, Weight      int
	Limits                Limits
	TextOnly              bool
}

type Pool struct {
	mu     sync.Mutex
	now    func() time.Time
	pick   func(int) int
	models map[string][]*connection
	local  map[string]*quota
	shared map[string]*quota
}

type connection struct {
	config Connection
	local  *quota
	shared *quota
}

type quota struct {
	limits        Limits
	active        int
	tokens        int64
	minute        []*reservation
	cooldownUntil time.Time
	probeActive   bool
}

type reservation struct {
	started time.Time
	tokens  int64
	probe   bool
}

type Usage struct {
	Active        int        `json:"active"`
	Requests      int        `json:"requests"`
	Tokens        int64      `json:"tokens"`
	Limits        Limits     `json:"limits"`
	CooldownUntil *time.Time `json:"cooldownUntil,omitempty"`
	Recovering    bool       `json:"recovering"`
}

type ConnectionCapacity struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	QuotaGroup string `json:"quotaGroup,omitempty"`
	Usage
}

type Capacity struct {
	Connections []ConnectionCapacity `json:"connections"`
	QuotaGroups map[string]Usage     `json:"quotaGroups"`
}

func (p *Pool) Capacity() Capacity {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	out := Capacity{Connections: []ConnectionCapacity{}, QuotaGroups: map[string]Usage{}}
	for _, connections := range p.models {
		for _, connection := range connections {
			config := connection.config
			out.Connections = append(out.Connections, ConnectionCapacity{config.ID, config.Model, config.QuotaGroup, connection.local.usage(now)})
		}
	}
	sort.Slice(out.Connections, func(i, j int) bool { return out.Connections[i].ID < out.Connections[j].ID })
	for name, group := range p.shared {
		if group.limits.valid() {
			out.QuotaGroups[name] = group.usage(now)
		}
	}
	return out
}

func (q *quota) usage(now time.Time) Usage {
	q.prune(now)
	usage := Usage{Active: q.active, Requests: len(q.minute), Tokens: q.tokens, Limits: q.limits, Recovering: q.probeActive}
	if now.Before(q.cooldownUntil) {
		deadline := q.cooldownUntil
		usage.CooldownUntil = &deadline
	}
	return usage
}

// NewPool builds an isolated scheduler from a validated configuration snapshot.
func NewPool(connections []Connection, groups map[string]Limits) (*Pool, error) {
	p := &Pool{now: time.Now, pick: rand.IntN, models: make(map[string][]*connection), local: make(map[string]*quota), shared: make(map[string]*quota, len(groups))}
	for id, limits := range groups {
		if id == "" || !limits.valid() {
			return nil, fmt.Errorf("invalid limits for quota group %q", id)
		}
		p.shared[id] = &quota{limits: limits}
	}
	seen := make(map[string]bool, len(connections))
	for _, config := range connections {
		if config.ID == "" || config.Model == "" || seen[config.ID] || !config.Limits.valid() ||
			config.Weight < 0 || config.Weight > 1000 {
			return nil, fmt.Errorf("invalid or duplicate connection %q", config.ID)
		}
		if config.Weight == 0 {
			config.Weight = 1
		}
		group := p.shared[config.QuotaGroup]
		if config.QuotaGroup != "" && group == nil {
			return nil, fmt.Errorf("connection %q references unknown quota group %q", config.ID, config.QuotaGroup)
		}
		seen[config.ID] = true
		p.local[config.ID] = &quota{limits: config.Limits}
		p.models[config.Model] = append(p.models[config.Model], &connection{
			config: config, local: p.local[config.ID], shared: group,
		})
	}
	return p, nil
}

// CarryCapacity copies recent usage into an unpublished replacement pool.
// The caller must prevent new admissions to previous while replacing it.
func (p *Pool) CarryCapacity(previous *Pool) {
	previous.mu.Lock()
	defer previous.mu.Unlock()
	now := p.now()
	carry := func(to, from map[string]*quota) {
		for id, old := range from {
			old.prune(now)
			next := to[id]
			if next == nil {
				if len(old.minute) == 0 && old.cooldownUntil.IsZero() {
					continue
				}
				next = &quota{}
				to[id] = next
			}
			next.minute = append([]*reservation(nil), old.minute...)
			next.tokens = old.tokens
			next.cooldownUntil = old.cooldownUntil
		}
	}
	carry(p.local, previous.local)
	carry(p.shared, previous.shared)
}

// Acquire reserves capacity at both the connection and its shared quota group.
// A preferred connection may be used for cache affinity within its priority.
func (p *Pool) Acquire(model string, estimatedTokens int, preferred string, needsTools bool, tried ...string) (*Lease, error) {
	if estimatedTokens <= 0 {
		return nil, errors.New("estimated tokens must be positive")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	candidates := p.models[model]
	if len(candidates) == 0 {
		return nil, ErrUnknownModel
	}
	now := p.now()
	var selected *connection
	var totalWeight int
	for _, candidate := range candidates {
		if (needsTools && candidate.config.TextOnly) || slices.Contains(tried, candidate.config.ID) || !candidate.local.available(now, estimatedTokens) ||
			(candidate.shared != nil && !candidate.shared.available(now, estimatedTokens)) {
			continue
		}
		if selected == nil {
			selected = candidate
			totalWeight = candidate.config.Weight
			continue
		}
		switch candidate.compareRank(selected, preferred) {
		case -1:
			selected = candidate
			totalWeight = candidate.config.Weight
		case 0:
			// Weighted reservoir sampling selects one of the equally ranked connections.
			totalWeight += candidate.config.Weight
			if p.pick(totalWeight) < candidate.config.Weight {
				selected = candidate
			}
		}
	}
	if selected == nil {
		return nil, ErrCapacity
	}
	lease := &Lease{pool: p, connection: selected, local: selected.local.reserve(now, estimatedTokens)}
	if selected.shared != nil {
		lease.shared = selected.shared.reserve(now, estimatedTokens)
	}
	return lease, nil
}

func (c *connection) compareRank(other *connection, preferred string) int {
	if c.config.Priority != other.config.Priority {
		return cmp.Compare(c.config.Priority, other.config.Priority)
	}
	if c.config.ID == preferred {
		return -1
	}
	if other.config.ID == preferred {
		return 1
	}
	load := float64(c.local.active) / float64(c.config.Limits.Concurrent)
	otherLoad := float64(other.local.active) / float64(other.config.Limits.Concurrent)
	return cmp.Compare(load, otherLoad)
}

func (q *quota) prune(now time.Time) {
	expired := 0
	for expired < len(q.minute) && !now.Before(q.minute[expired].started.Add(time.Minute)) {
		q.tokens -= q.minute[expired].tokens
		expired++
	}
	clear(q.minute[:expired])
	q.minute = q.minute[expired:]
}

func (q *quota) available(now time.Time, tokens int) bool {
	q.prune(now)
	return !now.Before(q.cooldownUntil) && !q.probeActive && q.active < q.limits.Concurrent &&
		len(q.minute) < q.limits.RequestsPerMinute && int64(tokens) <= int64(q.limits.TokensPerMinute)-q.tokens
}

func (q *quota) reserve(now time.Time, tokens int) *reservation {
	r := &reservation{started: now, tokens: int64(tokens), probe: !q.cooldownUntil.IsZero()}
	if r.probe {
		q.probeActive = true
	}
	q.active++
	q.tokens += r.tokens
	q.minute = append(q.minute, r)
	return r
}

type Lease struct {
	pool          *Pool
	connection    *connection
	local, shared *reservation
	finished      bool
}

func (l *Lease) ConnectionID() string { return l.connection.config.ID }

// Finish releases concurrent capacity after a completed stream. Negative
// usage keeps the original token reservation for the rest of the window.
func (l *Lease) Finish(actualTokens int) {
	l.finish(actualTokens, 0, 0, true)
}

// Interrupt releases the lease without treating a failed recovery probe as healthy.
func (l *Lease) Interrupt(actualTokens int) {
	var localRetry, sharedRetry time.Duration
	if l.local.probe {
		localRetry = defaultCooldown
	}
	if l.shared != nil && l.shared.probe {
		sharedRetry = defaultCooldown
	}
	l.finish(actualTokens, localRetry, sharedRetry, false)
}

// RateLimited is for an explicit upstream rejection before inference began.
// It keeps the request in the request rate window and releases reserved tokens.
func (l *Lease) RateLimited(retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = defaultCooldown
	}
	l.finish(0, retryAfter, retryAfter, false)
}

// AuthRejected cools this connection without blocking other credentials in its group.
func (l *Lease) AuthRejected() {
	l.finish(0, time.Minute, 0, false)
}

func (l *Lease) finish(actualTokens int, localRetryAfter, sharedRetryAfter time.Duration, completed bool) {
	l.pool.mu.Lock()
	defer l.pool.mu.Unlock()
	if l.finished {
		return
	}
	l.finished = true
	now := l.pool.now()
	l.connection.local.finish(now, l.local, actualTokens, localRetryAfter, completed)
	if l.shared != nil {
		l.connection.shared.finish(now, l.shared, actualTokens, sharedRetryAfter, completed)
	}
}

func (q *quota) finish(now time.Time, r *reservation, actualTokens int, retryAfter time.Duration, completed bool) {
	q.active--
	q.prune(now)
	if actualTokens >= 0 && slices.Contains(q.minute, r) {
		q.tokens += int64(actualTokens) - r.tokens
		r.tokens = int64(actualTokens)
	}
	if r.probe {
		q.probeActive = false
		// A concurrent rejection may have extended the cooldown.
		if completed && !now.Before(q.cooldownUntil) {
			q.cooldownUntil = time.Time{}
		}
	}
	if retryAfter > 0 && now.Add(retryAfter).After(q.cooldownUntil) {
		q.cooldownUntil = now.Add(retryAfter)
	}
}
