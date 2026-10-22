package httpx

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Event is one SSE message.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Broadcaster dispatches typed events to all subscribed SSE clients.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[chan Event]struct{}
}

// NewBroadcaster returns an empty broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{clients: make(map[chan Event]struct{})}
}

// Subscribe registers a buffered client channel and returns a cleanup function.
func (b *Broadcaster) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		close(ch)
	}
}

// Emit sends an event to every subscriber. Slow subscribers are dropped silently.
func (b *Broadcaster) Emit(event string, payload any) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- Event{Type: event, Payload: payload}:
		default:
			// Clients reconcile durable state after reconnecting.
		}
	}
}

// SSEHandler returns an http.HandlerFunc that streams events over SSE.
// It sends an initial "hello" event with the given snapshot payload.
func (b *Broadcaster) SSEHandler(helloPayload any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		// Send hello.
		hello, _ := json.Marshal(helloPayload)
		_, _ = w.Write([]byte("event: hello\ndata: "))
		_, _ = w.Write(hello)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()

		ch, unsub := b.Subscribe()
		defer unsub()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				payload, err := json.Marshal(ev.Payload)
				if err != nil {
					continue
				}
				_, _ = w.Write([]byte("event: " + ev.Type + "\ndata: "))
				_, _ = w.Write(payload)
				_, _ = w.Write([]byte("\n\n"))
				flusher.Flush()
			}
		}
	}
}
