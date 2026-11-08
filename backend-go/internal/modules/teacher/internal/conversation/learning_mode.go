package conversationstore

import (
	"encoding/json"
	"errors"
	"github.com/xmz14/lll/backend-go/internal/modules/learningprofile"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// LearningGuideline is immutable through product APIs and independent of the
// learner-editable system-prompt.txt. Legacy projects remain in free learning.
func (s *Store) LearningGuideline() (learningprofile.LearningGuideline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(s.root, "conversation", "learning-mode.json"))
	if errors.Is(err, os.ErrNotExist) {
		return learningprofile.NewLearningGuideline("general"), nil
	}
	if err != nil {
		return learningprofile.LearningGuideline{}, err
	}
	var saved learningprofile.LearningGuideline
	if len(data) > 32*1024 || !utf8.Valid(data) || json.Unmarshal(data, &saved) != nil || !learningprofile.ValidLearningMode(saved.LearningMode) || (saved.Version != 1 && saved.Version != 2) || saved.Guideline == "" {
		return saved, errors.New("invalid saved learning guideline")
	}
	return saved, nil
}
