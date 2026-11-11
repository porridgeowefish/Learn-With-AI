package store

import (
	"context"
	"database/sql"
	"time"
)

// UnlinkIdentity requires a remaining password login and keeps the initiating session.
func (s *Store) UnlinkIdentity(ctx context.Context, current Account, provider, verifiedHash string, digest []byte, now time.Time) (Account, error) {
	var account Account
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		account, err = sessionAccount(ctx, tx, digest, now)
		if err != nil {
			return err
		}
		var passwordCurrent bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account
			WHERE id=? AND username IS NOT NULL AND password_hash IS NOT NULL
			AND (?='email' OR password_hash=?))`, current.ID, provider, verifiedHash).
			Scan(&passwordCurrent); err != nil {
			return err
		}
		if account.ID != current.ID || !passwordCurrent {
			return ErrConflict
		}
		switch provider {
		case "email":
			if current.Email == "" || account.Email != current.Email {
				return ErrConflict
			}
			if _, err := tx.ExecContext(ctx, "UPDATE account SET email=NULL WHERE id=?", account.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM email_challenge WHERE (email=? AND purpose IN ('login','register','reset')) OR purpose=?",
				account.Email, "bind:"+account.ID); err != nil {
				return err
			}
			account.Email = ""
		case "github":
			result, err := tx.ExecContext(ctx, "DELETE FROM github_identity WHERE account_id=?", account.ID)
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if changed == 0 {
				return ErrConflict
			}
			account.GitHubLinked = false
		default:
			return ErrConflict
		}
		return revokeAccountAccess(ctx, tx, account.ID, digest)
	})
	return account, err
}
