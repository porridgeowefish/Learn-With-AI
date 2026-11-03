package conversationstore

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// PrepareImportedConversation seals interrupted replies and pauses queued items
// using the normal event contract while the project is still isolated.
func PrepareImportedConversation(root string) error {
	_, e := os.Stat(filepath.Join(root, "conversation", "events.jsonl"))
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	s := &Store{root: root, slug: filepath.Base(root), mu: new(sync.Mutex)}
	if _, e = s.ReconcileInterruptedResponses(); e != nil {
		return e
	}
	items, e := s.QueueItems()
	if e != nil {
		return e
	}
	if len(items) > 0 {
		return s.SetQueuePaused(true)
	}
	return nil
}
