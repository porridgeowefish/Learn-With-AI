package assetstore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
)

// FeynmanSession is one "讲给教师听" round: the learner explains, the teacher
// plays a curious novice and, on request, reviews the explanation against the
// review asset version that was current when the round started.
type FeynmanSession struct {
	SchemaVersion   int              `json:"schemaVersion"`
	SessionID       string           `json:"sessionId"`
	ReviewVersionID string           `json:"reviewVersionId,omitempty"`
	Messages        []FeynmanMessage `json:"messages"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
}

type FeynmanMessage struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"` // learner | teacher
	Content   string    `json:"content"`
	Status    string    `json:"status"` // completed | interrupted | failed
	CreatedAt time.Time `json:"createdAt"`
}

var ErrFeynmanSessionNotFound = errors.New("feynman session not found")
var ErrFeynmanReviewUnavailable = errors.New("此轮关联的复习稿已退役。请生成新复习稿后开始新一轮。")

const (
	maxFeynmanMessages      = 400
	maxFeynmanMessageLength = 20000
)

func (s *Store) feynmanDir() string { return filepath.Join(s.root, "assets", "feynman", "sessions") }

func validSessionID(id string) bool {
	return strings.HasPrefix(id, "feynman_") && filepath.Base(id) == id && !strings.ContainsAny(id, "/\\.")
}

func (s *Store) readFeynman(id string) (FeynmanSession, error) {
	if !validSessionID(id) {
		return FeynmanSession{}, ErrFeynmanSessionNotFound
	}
	var session FeynmanSession
	if err := readJSON(filepath.Join(s.feynmanDir(), id+".json"), &session); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FeynmanSession{}, ErrFeynmanSessionNotFound
		}
		return FeynmanSession{}, err
	}
	if session.SessionID != id {
		return FeynmanSession{}, ErrFeynmanSessionNotFound
	}
	return session, nil
}

// FeynmanSessions lists all rounds, oldest first.
func (s *Store) FeynmanSessions() ([]FeynmanSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.feynmanDir())
	if errors.Is(err, os.ErrNotExist) {
		return []FeynmanSession{}, nil
	}
	if err != nil {
		return nil, err
	}
	sessions := []FeynmanSession{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		session, err := s.readFeynman(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		if session.Messages == nil {
			session.Messages = []FeynmanMessage{}
		}
		sessions = append(sessions, session)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
			return sessions[i].SessionID < sessions[j].SessionID
		}
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})
	return sessions, nil
}

// FeynmanSession returns one round.
func (s *Store) FeynmanSession(id string) (FeynmanSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readFeynman(id)
}

// CreateFeynmanSession starts a new round pinned to the current review version.
func (s *Store) CreateFeynmanSession() (FeynmanSession, error) {
	review, err := s.Get("review")
	if err != nil {
		return FeynmanSession{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.feynmanDir(), 0o755); err != nil {
		return FeynmanSession{}, err
	}
	now := time.Now().UTC()
	session := FeynmanSession{SchemaVersion: 1, SessionID: idgen.New("feynman"), ReviewVersionID: review.Meta.CurrentVersionID, Messages: []FeynmanMessage{}, CreatedAt: now, UpdatedAt: now}
	if err := writeJSON(filepath.Join(s.feynmanDir(), session.SessionID+".json"), session); err != nil {
		return FeynmanSession{}, err
	}
	return session, nil
}

// AppendFeynmanMessage persists one learner or teacher message.
func (s *Store) AppendFeynmanMessage(id string, message FeynmanMessage) (FeynmanSession, error) {
	if message.Role != "learner" && message.Role != "teacher" {
		return FeynmanSession{}, errors.New("invalid feynman role")
	}
	if message.Status == "" {
		message.Status = "completed"
	}
	if message.Status != "completed" && message.Status != "interrupted" && message.Status != "failed" {
		return FeynmanSession{}, errors.New("invalid feynman status")
	}
	if len([]rune(message.Content)) > maxFeynmanMessageLength {
		message.Content = string([]rune(message.Content)[:maxFeynmanMessageLength])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.readFeynman(id)
	if err != nil {
		return FeynmanSession{}, err
	}
	if len(session.Messages) >= maxFeynmanMessages {
		return FeynmanSession{}, errors.New("feynman session is full; start a new round")
	}
	message.ID = idgen.New("fmsg")
	message.CreatedAt = time.Now().UTC()
	session.Messages = append(session.Messages, message)
	session.UpdatedAt = message.CreatedAt
	if err := writeJSON(filepath.Join(s.feynmanDir(), id+".json"), session); err != nil {
		return FeynmanSession{}, err
	}
	return session, nil
}

// ReviewContentForFeynman returns the HTML version this round was pinned to.
func (s *Store) ReviewContentForFeynman(session FeynmanSession) (string, string, error) {
	if session.ReviewVersionID != "" && validPracticeVersionID(session.ReviewVersionID) {
		s.mu.Lock()
		dir := filepath.Join(s.assetDir("review"), "versions", session.ReviewVersionID)
		var version Version
		versionErr := readJSON(filepath.Join(dir, "version.json"), &version)
		content, contentErr := os.ReadFile(filepath.Join(dir, "content.html"))
		s.mu.Unlock()
		if versionErr == nil && contentErr == nil && contentHash(string(content)) == version.ContentHash {
			return string(content), session.ReviewVersionID, nil
		}
	}
	return "", "", ErrFeynmanReviewUnavailable
}
