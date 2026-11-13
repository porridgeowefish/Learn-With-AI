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

func (s *Store) PublishPromptConfig(ctx context.Context, base int64, actor string, content json.RawMessage, now time.Time) (int64, error) {
	return s.publishConfig(ctx, "prompt_config_version", base, actor, content, now)
}

var ErrComparisonExperimentChanged = errors.New("published comparison model and variants are immutable")

func (s *Store) FillMissingReviewPrompt(ctx context.Context, text string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE prompt_config_version
		SET content=json_insert(content,'$.resources.review',?)
		WHERE json_type(content,'$.resources')='object' AND json_type(content,'$.resources.review') IS NULL`, text)
	return err
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
		if table == "prompt_config_version" {
			var changed bool
			err := tx.QueryRowContext(ctx, `WITH proposed(data) AS (VALUES(?))
				SELECT EXISTS(SELECT 1 FROM prompt_config_version,proposed
				WHERE json_extract(content,'$.comparison.id')=json_extract(data,'$.comparison.id')
				AND (json_extract(content,'$.comparison.model') IS NOT json_extract(data,'$.comparison.model')
				 OR json_extract(content,'$.comparison.a') IS NOT json_extract(data,'$.comparison.a')
				 OR json_extract(content,'$.comparison.b') IS NOT json_extract(data,'$.comparison.b')))`, string(content)).Scan(&changed)
			if err != nil {
				return err
			}
			if changed {
				return ErrComparisonExperimentChanged
			}
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
