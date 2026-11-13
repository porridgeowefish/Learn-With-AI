package conversationstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// FirstAnswerComparisonOperation reserves a project-scoped assignment key
// when claim is true. Check without claiming before asking the cloud to sample.
func (s *Store) FirstAnswerComparisonOperation(claim bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, events, err := s.loadLocked()
	if err != nil {
		return "", err
	}
	for _, event := range events {
		switch event.Type {
		case "teacher-comparison-dispatched":
			return "", nil
		case "message-recorded":
			var msg Message
			if json.Unmarshal(event.Data, &msg) == nil && visibleTeacherAnswer(msg) {
				return "", nil
			}
		}
	}
	digest := sha256.Sum256([]byte(meta.ID + "\x00" + s.slug))
	operation := "first-" + hex.EncodeToString(digest[:])
	if claim {
		if _, err := s.appendLocked("teacher-comparison-dispatched", map[string]string{"operationId": operation}); err != nil {
			return "", err
		}
	}
	return operation, nil
}

func visibleTeacherAnswer(msg Message) bool {
	if msg.Role != "teacher" || msg.OperationID == "teacher-welcome-v1" || msg.OperationID == "starter-methodology-v1" {
		return false
	}
	if msg.Comparison != nil {
		return true
	}
	for _, block := range msg.Blocks {
		if block.Type == "markdown" && strings.TrimSpace(block.Source) != "" {
			return true
		}
	}
	return false
}
