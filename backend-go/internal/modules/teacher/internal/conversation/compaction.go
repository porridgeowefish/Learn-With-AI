package conversationstore

import (
	"encoding/json"
	"strings"
)

type ContextCompaction struct {
	ThroughSeq uint64 `json:"throughSeq"`
	Summary    string `json:"summary"`
}

func (s *Store) ContextHistory(through uint64) ([]SequencedMessage, ContextCompaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return nil, ContextCompaction{}, err
	}
	messages := visibleMessages(events)
	hidden := hiddenTeacherMessages(events)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "context-compacted" {
			continue
		}
		var checkpoint ContextCompaction
		if json.Unmarshal(events[i].Data, &checkpoint) != nil || checkpoint.ThroughSeq == 0 ||
			checkpoint.ThroughSeq >= events[i].Seq || strings.TrimSpace(checkpoint.Summary) == "" ||
			(through > 0 && checkpoint.ThroughSeq >= through) {
			continue
		}
		// Regeneration can supersede a reply already covered by the summary.
		// Compare the visible conversation at commit with today's projection.
		stale := false
		if len(hidden) > 0 {
			for _, old := range visibleMessages(events[:i]) {
				if old.Seq > checkpoint.ThroughSeq {
					break
				}
				if hidden[old.Message.ID] {
					stale = true
					break
				}
			}
		}
		if !stale {
			return messages, checkpoint, nil
		}
	}
	return messages, ContextCompaction{}, nil
}

func (s *Store) RecordContextCompaction(checkpoint ContextCompaction) error {
	_, err := s.Append("context-compacted", checkpoint)
	return err
}
