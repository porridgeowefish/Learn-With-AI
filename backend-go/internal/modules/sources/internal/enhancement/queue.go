package enhancement

import "sync"

// Jobs are persisted before admission. Only lightweight references are queued,
// so a busy worker never loses an upload because a channel is full.
type taskQueue struct {
	sync.Mutex
	tasks []task
	keys  map[string]bool
	wake  chan struct{}
}

func newTaskQueue() *taskQueue {
	return &taskQueue{keys: map[string]bool{}, wake: make(chan struct{}, 1)}
}

func taskKey(t task) string { return t.slug + "/" + t.sourceID + "/" + t.job.ID }

func (q *taskQueue) push(t task) {
	q.Lock()
	key := taskKey(t)
	if q.keys[key] {
		q.Unlock()
		return
	}
	q.keys[key] = true
	q.tasks = append(q.tasks, t)
	q.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *taskQueue) next() task {
	for {
		q.Lock()
		if len(q.tasks) > 0 {
			t := q.tasks[0]
			q.tasks[0] = task{}
			q.tasks = q.tasks[1:]
			q.Unlock()
			return t
		}
		q.Unlock()
		<-q.wake
	}
}

func (q *taskQueue) done(t task) {
	q.Lock()
	delete(q.keys, taskKey(t))
	q.Unlock()
}
