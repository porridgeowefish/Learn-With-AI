package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// migratePasswordAccounts makes an email optional while preserving existing
// foreign keys, account IDs, sessions, balances and GitHub links.
func (s *Store) migratePasswordAccounts(ctx context.Context) (err error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 11 {
		return err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer func() {
		_, restoreErr := conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON")
		err = errors.Join(err, restoreErr)
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE account_next (
  id TEXT PRIMARY KEY,
  email TEXT UNIQUE,
  username TEXT UNIQUE,
  password_hash TEXT,
  created_at INTEGER NOT NULL,
  balance INTEGER NOT NULL CHECK(balance >= 0),
  CHECK(email IS NOT NULL OR username IS NOT NULL)
);
INSERT INTO account_next SELECT id,email,username,password_hash,created_at,balance FROM account;
DROP TABLE account;
ALTER TABLE account_next RENAME TO account;
PRAGMA user_version = 11;
`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := rows.Next()
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if invalid {
		return errors.New("cloud account migration violates a foreign key")
	}
	return tx.Commit()
}

func (s *Store) SetLoginCredentials(ctx context.Context, username, passwordHash string, sessionDigest []byte, now time.Time) (Account, error) {
	var account Account
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		account, err = sessionAccount(ctx, tx, sessionDigest, now)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE account SET username=?, password_hash=?
			WHERE id=? AND username IS NULL AND password_hash IS NULL
			AND NOT EXISTS (SELECT 1 FROM account WHERE username=?)`, username, passwordHash, account.ID, username)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		account.Name = username
		return nil
	})
	return account, err
}
