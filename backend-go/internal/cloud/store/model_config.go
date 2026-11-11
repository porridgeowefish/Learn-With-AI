package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const modelConfigSchema = `
CREATE TABLE model_config_version (
  version INTEGER PRIMARY KEY AUTOINCREMENT,
  actor TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
PRAGMA user_version = 7;
`

type ModelConfigVersion struct {
	Version   int64           `json:"version"`
	Config    json.RawMessage `json:"config"`
	CreatedAt int64           `json:"createdAt"`
}

func (s *Store) ActiveModelConfig(ctx context.Context) (ModelConfigVersion, error) {
	return readModelConfig(s.db.QueryRowContext(ctx, "SELECT version,content,created_at FROM model_config_version ORDER BY version DESC LIMIT 1"))
}

func (s *Store) ModelConfig(ctx context.Context, version int64) (ModelConfigVersion, error) {
	return readModelConfig(s.db.QueryRowContext(ctx, "SELECT version,content,created_at FROM model_config_version WHERE version=?", version))
}

func readModelConfig(row *sql.Row) (ModelConfigVersion, error) {
	var v ModelConfigVersion
	var content string
	err := row.Scan(&v.Version, &content, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	v.Config = json.RawMessage(content)
	return v, err
}

func (s *Store) PublishModelConfig(ctx context.Context, base int64, actor string, content json.RawMessage, now time.Time) (int64, error) {
	return s.publishConfig(ctx, "model_config_version", base, actor, content, now)
}

func (s *Store) ActivePromptConfig(ctx context.Context) (ModelConfigVersion, error) {
	return readModelConfig(s.db.QueryRowContext(ctx, "SELECT version,content,created_at FROM prompt_config_version ORDER BY version DESC LIMIT 1"))
}

func (s *Store) PromptConfig(ctx context.Context, version int64) (ModelConfigVersion, error) {
	return readModelConfig(s.db.QueryRowContext(ctx, "SELECT version,content,created_at FROM prompt_config_version WHERE version=?", version))
}

func (s *Store) PromptConfigsForExperiments(ctx context.Context, ids []string) ([]json.RawMessage, error) {
	keys, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.content FROM prompt_config_version p WHERE EXISTS (
		SELECT 1 FROM json_each(p.content,'$.experiments') AS e
		WHERE json_extract(e.value,'$.id') IN (SELECT value FROM json_each(?))
	)`, string(keys))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []json.RawMessage
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		configs = append(configs, json.RawMessage(content))
	}
	return configs, rows.Err()
}

type RetiredPromptConfig struct {
	Config    json.RawMessage
	RetiredAt time.Time
}

// RetiredPromptConfigs selects versions by the time they stopped being active,
// including a long-lived version whose own publication predates the window.
func (s *Store) RetiredPromptConfigs(ctx context.Context, since time.Time) ([]RetiredPromptConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT prior.content, successor.created_at
		FROM prompt_config_version successor
		JOIN prompt_config_version prior ON prior.version = (
			SELECT MAX(version) FROM prompt_config_version WHERE version < successor.version
		)
		WHERE successor.created_at >= ? ORDER BY successor.version DESC`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []RetiredPromptConfig
	for rows.Next() {
		var content string
		var retired int64
		if err := rows.Scan(&content, &retired); err != nil {
			return nil, err
		}
		configs = append(configs, RetiredPromptConfig{json.RawMessage(content), time.Unix(retired, 0)})
	}
	return configs, rows.Err()
}

func (s *Store) PublishPromptConfig(ctx context.Context, base int64, actor string, content json.RawMessage, now time.Time) (int64, error) {
	return s.publishConfig(ctx, "prompt_config_version", base, actor, content, now)
}

func (s *Store) publishConfig(ctx context.Context, table string, base int64, actor string, content json.RawMessage, now time.Time) (int64, error) {
	if !json.Valid(content) || actor == "" {
		return 0, ErrConflict
	}
	var version int64
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var current int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM "+table).Scan(&current); err != nil {
			return err
		}
		if current != base {
			return ErrConflict
		}
		row, err := tx.ExecContext(ctx, "INSERT INTO "+table+"(actor,content,created_at) VALUES(?,?,?)", actor, string(content), now.Unix())
		if err != nil {
			return err
		}
		version, err = row.LastInsertId()
		return err
	})
	return version, err
}

const promptConfigSchema = `
CREATE TABLE prompt_config_version (
  version INTEGER PRIMARY KEY AUTOINCREMENT,
  actor TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
PRAGMA user_version = 9;
`
