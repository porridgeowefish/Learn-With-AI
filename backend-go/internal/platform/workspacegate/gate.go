// Package workspacegate keeps workspace transfers exclusive with requests and workers.
package workspacegate

import (
	"context"
	"errors"
	"sync"
)

var ErrBusy = errors.New("工作区正在转移，请稍后重试")
var ErrRetired = errors.New("工作区已暂停读写，请重新打开应用")

var state = struct {
	sync.Mutex
	active             int
	exclusive, retired bool
	changed            chan struct{}
}{changed: make(chan struct{})}

func signal() { close(state.changed); state.changed = make(chan struct{}) }

// Enter admits one normal request, or reports that maintenance is in progress.
func Enter() (func(), error) {
	state.Lock()
	defer state.Unlock()
	if state.retired {
		return nil, ErrRetired
	}
	if state.exclusive {
		return nil, ErrBusy
	}
	state.active++
	return leave, nil
}
func leave() {
	state.Lock()
	defer state.Unlock()
	state.active--
	if state.active == 0 {
		signal()
	}
}

// Continue pins detached work before its parent request or worker returns.
func Continue() func() { state.Lock(); defer state.Unlock(); state.active++; return leave }

// Worker waits for a running transfer before starting a new unit of work.
func Worker(ctx context.Context, work func()) {
	for {
		state.Lock()
		if state.retired || ctx.Err() != nil {
			state.Unlock()
			return
		}
		if !state.exclusive {
			state.active++
			state.Unlock()
			defer leave()
			work()
			return
		}
		changed := state.changed
		state.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-changed:
		}
	}
}

// Exclusive closes admissions and waits for operations already admitted.
// The caller passes retired=true on release after committing a relocation.
func Exclusive(ctx context.Context) (func(bool), error) {
	state.Lock()
	if state.retired {
		state.Unlock()
		return nil, ErrRetired
	}
	if state.exclusive {
		state.Unlock()
		return nil, ErrBusy
	}
	state.exclusive = true
	release := func(retired bool) {
		state.Lock()
		defer state.Unlock()
		state.exclusive = false
		state.retired = retired
		signal()
	}
	for state.active > 0 {
		changed := state.changed
		state.Unlock()
		select {
		case <-ctx.Done():
			release(false)
			return nil, ctx.Err()
		case <-changed:
		}
		state.Lock()
	}
	state.Unlock()
	return release, nil
}
