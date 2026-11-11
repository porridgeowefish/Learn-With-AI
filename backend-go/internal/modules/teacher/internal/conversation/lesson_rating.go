package conversationstore

import (
	"encoding/json"
	"errors"
)

var ErrLessonAlreadyRated = errors.New("teaching session rating already recorded")

type lessonRating struct {
	MessageID string `json:"messageId"`
	Stars     int    `json:"stars"`
}

func readLessonRatings(events []Event) map[string]int {
	ratings := make(map[string]int)
	for _, event := range events {
		if event.Type == "lesson-rating-recorded" {
			var rating lessonRating
			if json.Unmarshal(event.Data, &rating) == nil {
				ratings[rating.MessageID] = rating.Stars
			}
		}
	}
	return ratings
}

func applyLessonRatings(events []Event, messages []Message) {
	ratings := readLessonRatings(events)
	for i := range messages {
		if stars, ok := ratings[messages[i].LessonRatingKey()]; ok {
			messages[i].LessonRating = &stars
		}
	}
}

func (m Message) LessonRatingKey() string {
	if m.LessonEndID != "" {
		return m.LessonEndID
	}
	return m.ID
}

// SaveLessonRating stores the score locally. Zero records an explicit skip.
func (s *Store) SaveLessonRating(id string, stars int, submit func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, events, err := s.loadLocked()
	if err != nil {
		return err
	}
	if saved, ok := readLessonRatings(events)[id]; ok {
		if saved != stars {
			return ErrLessonAlreadyRated
		}
		return nil
	}
	if submit != nil {
		if err := submit(); err != nil {
			return err
		}
	}
	_, err = s.appendLocked("lesson-rating-recorded", lessonRating{MessageID: id, Stars: stars})
	return err
}
