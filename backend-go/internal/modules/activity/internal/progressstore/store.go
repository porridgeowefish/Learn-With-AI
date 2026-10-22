// Package progressstore persists project-local, append-only growth events.
package progressstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	filesystem "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
)

const PolicyVersion = "practice-v1"
const LearningPolicyVersion = "learning-v1"

type Event struct {
	ID            string `json:"id"`
	SourceType    string `json:"sourceType"`
	SourceID      string `json:"sourceId"`
	AttemptID     string `json:"attemptId"`
	Difficulty    int    `json:"difficulty"`
	Outcome       string `json:"outcome"`
	Delta         int    `json:"delta"`
	ActivityDelta int    `json:"activityDelta,omitempty"`
	Title         string `json:"title,omitempty"`
	Detail        string `json:"detail,omitempty"`
	PolicyVersion string `json:"policyVersion"`
	CreatedAt     string `json:"createdAt"`
}

type Summary struct {
	Total        int            `json:"total"`
	BySource     map[string]int `json:"bySource"`
	RecentEvents []Event        `json:"recentEvents"`
	UpdatedAt    string         `json:"updatedAt"`
}

type Store struct {
	root string
	mu   *sync.Mutex
}

var rootLocks sync.Map

func rootLock(root string) *sync.Mutex {
	lock, _ := rootLocks.LoadOrStore(root, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func New(slug string) (*Store, error) {
	if _, err := workspace.ReadProjectState(slug); err != nil {
		return nil, err
	}
	root, err := workspace.ProjectRootForSlug(slug)
	if err != nil {
		return nil, err
	}
	progressRoot := filepath.Join(root, "progress")
	return &Store{root: progressRoot, mu: rootLock(progressRoot)}, nil
}

func (s *Store) ReadEvents() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readEvents()
}

func (s *Store) readEvents() ([]Event, error) {
	f, err := os.Open(filepath.Join(s.root, "events.jsonl"))
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("parse progress event: %w", err)
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

// Award appends one event unless the deterministic event id already exists.
func (s *Store) Award(event Event) (Event, bool, Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events, err := s.readEvents()
	if err != nil {
		return Event{}, false, Summary{}, err
	}
	for _, existing := range events {
		if existing.ID == event.ID {
			return existing, false, summarize(events), nil
		}
	}
	if event.PolicyVersion == "" {
		event.PolicyVersion = PolicyVersion
	}
	if event.CreatedAt == "" {
		event.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	events = append(events, event)
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return Event{}, false, Summary{}, err
	}
	f, err := os.OpenFile(filepath.Join(s.root, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Event{}, false, Summary{}, err
	}
	line, _ := json.Marshal(event)
	_, writeErr := f.Write(append(line, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return Event{}, false, Summary{}, writeErr
	}
	if closeErr != nil {
		return Event{}, false, Summary{}, closeErr
	}
	summary := summarize(events)
	raw, _ := json.MarshalIndent(summary, "", "  ")
	if err := filesystem.AtomicWriteFile(filepath.Join(s.root, "summary.json"), raw, 0o644); err != nil {
		return Event{}, false, Summary{}, err
	}
	return event, true, summary, nil
}

func (s *Store) Summary() (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events, err := s.readEvents()
	if err != nil {
		return Summary{}, err
	}
	return summarize(events), nil
}

func summarize(events []Event) Summary {
	out := Summary{BySource: map[string]int{}}
	for _, event := range events {
		out.Total += event.Delta
		out.BySource[event.SourceType] += event.Delta
	}
	recent := append([]Event(nil), events...)
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].CreatedAt > recent[j].CreatedAt })
	if len(recent) > 20 {
		recent = recent[:20]
	}
	out.RecentEvents = recent
	out.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return out
}
