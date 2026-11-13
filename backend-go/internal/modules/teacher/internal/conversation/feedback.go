package conversationstore

import (
	"encoding/json"
	"errors"
	"strings"
)

var ErrFeedbackNotFound = errors.New("teacher answer unavailable")
var ErrComparisonPending = errors.New("select a comparison answer first")

type AnswerComparison struct {
	ID   string `json:"id"`
	A    string `json:"a"`
	B    string `json:"b"`
	Vote string `json:"vote,omitempty"`
}

type answerFeedback struct {
	MessageID string `json:"messageId"`
	Value     string `json:"value"`
}

func feedbackByMessage(events []Event) (map[string]string, map[string]string) {
	feedback, votes := make(map[string]string), make(map[string]string)
	for _, event := range events {
		if event.Type != "answer-feedback" && event.Type != "answer-comparison-voted" {
			continue
		}
		var item answerFeedback
		if json.Unmarshal(event.Data, &item) != nil {
			continue
		}
		if event.Type == "answer-feedback" {
			feedback[item.MessageID] = item.Value
		} else {
			votes[item.MessageID] = item.Value
		}
	}
	return feedback, votes
}

func applyAnswerFeedback(message *Message, feedback, votes map[string]string) {
	message.Feedback = feedback[message.ID]
	if message.Comparison == nil {
		return
	}
	if vote := votes[message.ID]; vote != "" {
		message.Comparison.Vote = vote
		answer := message.Comparison.A
		if vote == "b" {
			answer = message.Comparison.B
		}
		message.Blocks = []Block{{ID: message.ID + "-answer", Type: "markdown", Source: answer}}
	}
}

func (s *Store) PendingComparison() (bool, error) {
	messages, err := s.AllMessages()
	if err != nil || len(messages) == 0 {
		return false, err
	}
	message := messages[len(messages)-1]
	return message.Comparison != nil && message.Comparison.Vote == "", nil
}

func (s *Store) SaveAnswerFeedback(id, value string) error {
	if value != "up" && value != "down" {
		return errors.New("invalid answer feedback")
	}
	return s.recordAnswerFeedback(id, value, nil)
}

func (s *Store) SaveComparisonVote(id, value string, submit func(string) error) error {
	if value != "a" && value != "b" && value != "tie" && value != "skipped" {
		return errors.New("invalid comparison vote")
	}
	return s.recordAnswerFeedback(id, value, submit)
}

func (s *Store) recordAnswerFeedback(id, value string, submit func(string) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return err
	}
	for _, item := range visibleMessages(events) {
		m := item.Message
		if m.ID != id || m.Role != "teacher" {
			continue
		}
		eventType := "answer-feedback"
		if submit != nil {
			if m.Comparison == nil {
				return ErrFeedbackNotFound
			}
			if m.Comparison.Vote == value {
				return nil
			}
			if m.Comparison.Vote != "" {
				return ErrComparisonPending
			}
			if value != "skipped" {
				if err := submit(m.Comparison.ID); err != nil {
					return err
				}
			}
			eventType = "answer-comparison-voted"
		} else {
			if m.Feedback == value {
				return nil
			}
			visible := false
			for _, b := range m.Blocks {
				visible = visible || (b.Type == "markdown" && strings.TrimSpace(b.Source) != "")
			}
			if !visible || (m.Status != "completed" && m.Status != "interrupted") {
				return ErrFeedbackNotFound
			}
		}
		_, err = s.appendLocked(eventType, answerFeedback{MessageID: id, Value: value})
		return err
	}
	return ErrFeedbackNotFound
}
