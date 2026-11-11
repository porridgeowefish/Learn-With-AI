package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) migrateTeachingRatings(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 17 {
			return err
		}
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS prompt_teaching_rating (
  account_id TEXT NOT NULL,
  exposure_id TEXT NOT NULL,
  experiment TEXT NOT NULL,
  stars INTEGER CHECK(stars BETWEEN 1 AND 5),
  rated_at INTEGER,
  PRIMARY KEY(account_id,exposure_id,experiment),
  FOREIGN KEY(account_id,exposure_id,experiment) REFERENCES prompt_exposure(account_id,exposure_id,experiment)
) WITHOUT ROWID;
PRAGMA user_version = 17;`)
		return err
	})
}

// RateTeaching links a score to a successfully observed teaching exposure.
// A retry with the same score is idempotent. An ambiguous exposure is rejected.
func (s *Store) RateTeaching(ctx context.Context, accountID, exposureID string, stars int, now time.Time) error {
	if stars < 1 || stars > 5 {
		return ErrConflict
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var count int
		var saved sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*),MAX(stars) FROM prompt_teaching_rating
			WHERE account_id=? AND exposure_id=?`, accountID, exposureID).Scan(&count, &saved)
		if err != nil {
			return err
		}
		switch {
		case count == 0:
			return ErrNotFound
		case count != 1 || (saved.Valid && saved.Int64 != int64(stars)):
			return ErrConflict
		case saved.Valid:
			return nil
		}
		result, err := tx.ExecContext(ctx, `UPDATE prompt_teaching_rating SET stars=?,rated_at=?
			WHERE account_id=? AND exposure_id=? AND stars IS NULL`, stars, now.Unix(), accountID, exposureID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err == nil && n != 1 {
			return errors.New("teaching rating was not stored")
		}
		return err
	})
}
