package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
)

type routeHint struct {
	connection string
	expires    time.Time
}

type routeAffinity struct {
	mu    sync.Mutex
	hints map[[sha256.Size]byte]routeHint
}

func routeKey(account string, in inferenceInput) [sha256.Size]byte {
	// The remaining history, turn instructions and tool results can change between calls.
	prefix, _ := json.Marshal(struct {
		Account, Model, System string
		First                  ai.GatewayMessage
	}{account, in.Model, in.System, in.Messages[0]})
	return sha256.Sum256(prefix)
}

func (a *routeAffinity) preferred(key [sha256.Size]byte, now time.Time) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	hint := a.hints[key]
	if !now.Before(hint.expires) {
		delete(a.hints, key)
		return ""
	}
	return hint.connection
}

func (a *routeAffinity) remember(key [sha256.Size]byte, connection string, now time.Time) {
	const maxHints = 4096
	const ttl = 15 * time.Minute
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hints == nil {
		a.hints = make(map[[sha256.Size]byte]routeHint)
	}
	if _, exists := a.hints[key]; !exists && len(a.hints) >= maxHints {
		for existing, hint := range a.hints {
			if !now.Before(hint.expires) {
				delete(a.hints, existing)
			}
		}
		if len(a.hints) >= maxHints {
			for existing := range a.hints {
				delete(a.hints, existing)
				break
			}
		}
	}
	a.hints[key] = routeHint{connection: connection, expires: now.Add(ttl)}
}
