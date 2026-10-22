package teacherservice

import (
	"context"
	"sync"
	"time"
)

// ActiveResponse owns a provider response independently from an HTTP connection.
type ActiveResponse struct {
	cancel   context.CancelFunc
	mu       sync.Mutex
	frames   []StreamFrame
	done     bool
	finished chan struct{}
	notify   chan struct{}
}

func (r *ActiveResponse) Append(frame StreamFrame) {
	r.mu.Lock()
	r.frames = append(r.frames, frame)
	close(r.notify)
	r.notify = make(chan struct{})
	r.mu.Unlock()
}

// Done is closed exactly once when the response reaches its terminal state.
// Steering waits on it so a new turn never races the old one's persistence.
func (r *ActiveResponse) Done() <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished == nil {
		r.finished = make(chan struct{})
	}
	return r.finished
}

func (r *ActiveResponse) Finish() {
	r.mu.Lock()
	if !r.done {
		r.done = true
		if r.finished == nil {
			r.finished = make(chan struct{})
		}
		close(r.finished)
		close(r.notify)
	}
	r.mu.Unlock()
}

func (r *ActiveResponse) Snapshot(after int) ([]StreamFrame, bool, <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if after < 0 || after > len(r.frames) {
		after = 0
	}
	return append([]StreamFrame(nil), r.frames[after:]...), r.done, r.notify
}

func (r *ActiveResponse) Stop() { r.cancel() }

type ActiveResponses struct {
	closed    bool
	mu        sync.RWMutex
	byID      map[string]*ActiveResponse
	byProject map[string]*ActiveResponse
}

func NewActiveResponses() *ActiveResponses {
	return &ActiveResponses{byID: map[string]*ActiveResponse{}, byProject: map[string]*ActiveResponse{}}
}

func (a *ActiveResponses) Start(projectSlug string) (context.Context, *ActiveResponse, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.byProject[projectSlug] != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	response := &ActiveResponse{cancel: cancel, notify: make(chan struct{})}
	a.byProject[projectSlug] = response
	return ctx, response, true
}

func (a *ActiveResponses) Bind(responseID string, response *ActiveResponse) {
	a.mu.Lock()
	a.byID[responseID] = response
	a.mu.Unlock()
}

func (a *ActiveResponses) Finish(projectSlug, responseID string, response *ActiveResponse) {
	a.mu.Lock()
	if responseID != "" {
		delete(a.byID, responseID)
	}
	if a.byProject[projectSlug] == response {
		delete(a.byProject, projectSlug)
	}
	a.mu.Unlock()
	response.Finish()
}

func (a *ActiveResponses) Project(projectSlug string) *ActiveResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.byProject[projectSlug]
}

func (a *ActiveResponses) Response(responseID string) *ActiveResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.byID[responseID]
}

func (a *ActiveResponses) StopAll() {
	a.mu.Lock()
	a.closed = true
	var runs []*ActiveResponse
	for _, r := range a.byProject {
		runs = append(runs, r)
		r.Stop()
	}
	a.mu.Unlock()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for _, r := range runs {
		select {
		case <-r.Done():
		case <-timer.C:
			return
		}
	}
}
