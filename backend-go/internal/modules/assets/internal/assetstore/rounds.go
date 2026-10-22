package assetstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Practice rounds reuse immutable asset versions. Learner edits belong to the
// same round; a generated version marks the start of another exercise set.
type PracticeRound struct {
	VersionID string    `json:"versionId"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Store) PracticeRounds() ([]PracticeRound, error) {
	if _, err := s.Get("practice"); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.root, "assets", "practice")
	meta, err := readMeta(filepath.Join(dir, "asset.json"))
	if err != nil {
		return nil, err
	}
	rounds := []PracticeRound{}
	seen := map[string]bool{}
	selected := false
	for id := meta.CurrentVersionID; id != ""; {
		if seen[id] || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, "/\\") {
			return nil, errors.New("invalid practice lineage")
		}
		seen[id] = true
		var v Version
		if err := readJSON(filepath.Join(dir, "versions", id, "version.json"), &v); err != nil {
			return nil, err
		}
		content, err := os.ReadFile(filepath.Join(dir, "versions", id, "content.md"))
		if err != nil {
			return nil, err
		}
		if contentHash(string(content)) != v.ContentHash {
			return nil, errors.New("practice version hash mismatch")
		}
		if !selected && (strings.TrimSpace(string(content)) != "" || v.Author == "learner") {
			rounds = append(rounds, PracticeRound{VersionID: id, Content: string(content), CreatedAt: v.CreatedAt})
			selected = true
		}
		if v.Author != "learner" {
			selected = false
		}
		id = v.ParentVersionID
	}
	for i, j := 0, len(rounds)-1; i < j; i, j = i+1, j-1 {
		rounds[i], rounds[j] = rounds[j], rounds[i]
	}
	return rounds, nil
}
