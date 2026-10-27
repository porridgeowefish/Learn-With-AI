package conversationstore

import "encoding/json"

func replayQueuePaused(events []Event) bool {
	paused := false
	for _, event := range events {
		if event.Type == "queue-pause-changed" {
			var data struct {
				Paused bool `json:"paused"`
			}
			if json.Unmarshal(event.Data, &data) == nil {
				paused = data.Paused
			}
		}
	}
	return paused
}

// QueuePaused is durable across browser refresh and service restart.
func (s *Store) QueuePaused() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	return replayQueuePaused(events), err
}

func (s *Store) SetQueuePaused(paused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return err
	}
	if replayQueuePaused(events) == paused {
		return nil
	}
	_, err = s.appendLocked("queue-pause-changed", map[string]any{"paused": paused})
	return err
}
