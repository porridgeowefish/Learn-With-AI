package store

import (
	"context"
	"database/sql"
	"time"
)

func (s *Store) migrateReplyFeedback(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 24 {
			return err
		}
		_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS reply_feedback (
			reply_id TEXT PRIMARY KEY, model TEXT NOT NULL,
			value TEXT NOT NULL CHECK(value IN ('up','down')),
			revision INTEGER NOT NULL CHECK(revision > 0), updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS reply_feedback_recent ON reply_feedback(updated_at DESC,reply_id);
		PRAGMA user_version=24;`)
		return err
	})
}

func (s *Store) SaveReplyFeedback(ctx context.Context, id, model, value string, revision uint64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reply_feedback(reply_id,model,value,revision,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(reply_id) DO UPDATE SET model=excluded.model,value=excluded.value,
		revision=excluded.revision,updated_at=excluded.updated_at WHERE excluded.revision>reply_feedback.revision`,
		id, model, value, revision, now.UnixMilli())
	return err
}

type ReplyFeedback struct {
	ReplyID   string `json:"replyId"`
	Model     string `json:"model"`
	Value     string `json:"value"`
	UpdatedAt int64  `json:"updatedAt"`
}

func (s *Store) ReplyFeedback(ctx context.Context, value string, offset int) ([]ReplyFeedback, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT reply_id,model,value,updated_at FROM reply_feedback
		WHERE (?='' OR value=?) ORDER BY updated_at DESC,reply_id LIMIT 51 OFFSET ?`, value, value, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReplyFeedback{}
	for rows.Next() {
		var row ReplyFeedback
		if err := rows.Scan(&row.ReplyID, &row.Model, &row.Value, &row.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
