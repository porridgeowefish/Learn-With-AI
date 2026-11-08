package teachergateway

import (
	"context"
	"crypto/sha256"
	"fmt"
	askaiconfig "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	"strings"
	"sync"
)

type backgroundKey struct{}

func WithBackground(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

type admission struct {
	mu              sync.Mutex
	fg, bg, waiting int
	changed         chan struct{}
}

var admissions sync.Map

func admissionFor(id string) *admission {
	v, _ := admissions.LoadOrStore(id, &admission{changed: make(chan struct{})})
	return v.(*admission)
}
func (a *admission) signal() { close(a.changed); a.changed = make(chan struct{}) }
func (a *admission) enter(ctx context.Context, background bool, fgMax, bgMax, total int) (func(), error) {
	if fgMax < 1 || fgMax > 32 || bgMax < 1 || bgMax > 32 || total < 1 || total > 64 {
		return nil, fmt.Errorf("invalid model concurrency limits")
	}
	a.mu.Lock()
	if !background {
		a.waiting++
	}
	for {
		if err := ctx.Err(); err != nil {
			if !background {
				a.waiting--
				a.signal()
			}
			a.mu.Unlock()
			return nil, err
		}
		room := a.fg+a.bg < total
		if background {
			room = room && a.bg < bgMax && a.waiting == 0 && (total == 1 || a.bg < max(1, total-fgMax))
		} else {
			room = room && a.fg < fgMax
		}
		if room {
			if background {
				a.bg++
			} else {
				a.waiting--
				a.fg++
			}
			a.mu.Unlock()
			return func() {
				a.mu.Lock()
				if background {
					a.bg--
				} else {
					a.fg--
				}
				a.signal()
				a.mu.Unlock()
			}, nil
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		a.mu.Lock()
	}
}
func acquireProvider(ctx context.Context, p askaiconfig.Provider) (func(), error) {
	cfg, err := askaiconfig.Load()
	if err != nil {
		return nil, err
	}
	fg, bg, total := 2, 1, p.MaxConcurrency
	if cfg != nil {
		if cfg.ForegroundConcurrency != 0 {
			fg = cfg.ForegroundConcurrency
		}
		if cfg.BackgroundConcurrency != 0 {
			bg = cfg.BackgroundConcurrency
		}
		if configured := cfg.FindExact(p.ID); configured != nil && configured.MaxConcurrency != 0 {
			total = configured.MaxConcurrency
		}
	}
	if total == 0 {
		total = fg + bg
	}
	key := p.ID
	if key == "" {
		key = fmt.Sprintf("explicit:%x", sha256.Sum256([]byte(strings.TrimRight(p.BaseURL, "/")+"\x00"+p.Model)))
	}
	background, _ := ctx.Value(backgroundKey{}).(bool)
	return admissionFor(key).enter(ctx, background, fg, bg, total)
}
