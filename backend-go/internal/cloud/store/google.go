package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) migrateGoogleIdentity(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 18 {
			return err
		}
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS google_identity (
  subject TEXT PRIMARY KEY,
  account_id TEXT UNIQUE NOT NULL REFERENCES account(id)
);
PRAGMA user_version = 18;`)
		return err
	})
}

// SignInGoogle accepts only identities and addresses verified by Google.
func (s *Store) SignInGoogle(ctx context.Context, subject, email string, digest []byte, now, expires time.Time) (Account, error) {
	var account Account
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var accountID string
		err := tx.QueryRowContext(ctx, "SELECT account_id FROM google_identity WHERE subject=?", subject).Scan(&accountID)
		if errors.Is(err, sql.ErrNoRows) {
			linked, err := accountForEmail(ctx, tx, email, now)
			if err != nil {
				return err
			}
			accountID = linked.ID
			result, err := tx.ExecContext(ctx, "INSERT INTO google_identity VALUES(?,?) ON CONFLICT DO NOTHING", subject, accountID)
			if err != nil {
				return err
			}
			inserted, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if inserted != 1 {
				return ErrConflict
			}
		} else if err != nil {
			return err
		}
		if err := issueSignedInSession(ctx, tx, accountID, digest, now, expires); err != nil {
			return err
		}
		account, err = sessionAccount(ctx, tx, digest, now)
		return err
	})
	return account, err
}
