package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const learningPointSchemaVersion = 25

var ErrInvalidLearningPointRules = errors.New("invalid learning point rules")

type LearningPointValues struct {
	PracticePoints     int64 `json:"practicePoints"`
	ConversationPoints int64 `json:"conversationPoints"`
}

type LearningPointRules struct {
	Version int64 `json:"version"`
	LearningPointValues
}

func (s *Store) migrateLearningPointRules(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= learningPointSchemaVersion {
			return err
		}
		_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS learning_point_rules_version (
			version INTEGER PRIMARY KEY AUTOINCREMENT,
			actor TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);
		PRAGMA user_version = 25;`)
		return err
	})
}

func (s *Store) ActiveLearningPointRules(ctx context.Context) (LearningPointRules, error) {
	row, err := readModelConfig(s.db.QueryRowContext(ctx, `SELECT version, content, created_at
		FROM learning_point_rules_version ORDER BY version DESC LIMIT 1`))
	if errors.Is(err, ErrNotFound) {
		return LearningPointRules{LearningPointValues: LearningPointValues{PracticePoints: 10, ConversationPoints: 1}}, nil
	}
	if err != nil {
		return LearningPointRules{}, err
	}
	result := LearningPointRules{Version: row.Version}
	if err := json.Unmarshal(row.Config, &result.LearningPointValues); err != nil {
		return LearningPointRules{}, err
	}
	return result, nil
}

func (s *Store) SaveLearningPointRules(ctx context.Context, base int64, actor string, values LearningPointValues, now time.Time) (LearningPointRules, error) {
	const maxPoints = 1_000_000
	if base < 0 || values.PracticePoints < 0 || values.ConversationPoints < 0 || values.PracticePoints > maxPoints || values.ConversationPoints > maxPoints {
		return LearningPointRules{}, ErrInvalidLearningPointRules
	}
	content, err := json.Marshal(values)
	if err != nil {
		return LearningPointRules{}, err
	}
	version, err := s.publishConfig(ctx, "learning_point_rules_version", base, actor, content, now)
	if err != nil {
		return LearningPointRules{}, err
	}
	return LearningPointRules{Version: version, LearningPointValues: values}, nil
}
