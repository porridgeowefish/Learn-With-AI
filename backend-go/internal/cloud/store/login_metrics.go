package store

import (
	"context"
	"database/sql"
	"time"
)

func (s *Store) migrateLoginMetrics(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 16 {
			return err
		}
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS account_login_day (
  day TEXT NOT NULL,
  account_id TEXT NOT NULL REFERENCES account(id),
  PRIMARY KEY(day,account_id)
) WITHOUT ROWID;
PRAGMA user_version = 16;`)
		return err
	})
}

func recordLoginDay(ctx context.Context, tx *sql.Tx, accountID string, at time.Time) error {
	day := at.In(shanghai)
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_login_day(day,account_id) VALUES(?,?)
		ON CONFLICT(day,account_id) DO NOTHING`, day.Format("2006-01-02"), accountID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM account_login_day WHERE day < ?", day.AddDate(0, 0, -89).Format("2006-01-02"))
	return err
}

// issueSignedInSession records a successful login atomically with its session.
func issueSignedInSession(ctx context.Context, tx *sql.Tx, accountID string, digest []byte, now, expires time.Time) error {
	if _, err := tx.ExecContext(ctx, "INSERT INTO account_session VALUES(?,?,?)", digest, accountID, expires.Unix()); err != nil {
		return err
	}
	return recordLoginDay(ctx, tx, accountID, now)
}
