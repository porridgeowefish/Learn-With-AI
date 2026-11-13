package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrCooldown = errors.New("email code recently requested")
	ErrCode     = errors.New("invalid or expired email code")
)

const authSchema = `
CREATE TABLE email_challenge (
  email TEXT NOT NULL,
  purpose TEXT NOT NULL,
  digest BLOB NOT NULL,
  sent_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  failures INTEGER NOT NULL DEFAULT 0,
  used INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(email,purpose)
);
CREATE TABLE account_session (
  digest BLOB PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES account(id),
  expires_at INTEGER NOT NULL
);
CREATE INDEX account_sessions ON account_session(account_id);
PRAGMA user_version = 2;
`

func (s *Store) PutCode(ctx context.Context, email, purpose string, digest []byte, now, expires time.Time) error {
	result, err := s.db.ExecContext(ctx, `INSERT INTO email_challenge(email,purpose,digest,sent_at,expires_at) VALUES(?,?,?,?,?)
		ON CONFLICT(email,purpose) DO UPDATE SET digest=excluded.digest, sent_at=excluded.sent_at, expires_at=excluded.expires_at, failures=0, used=0
		WHERE email_challenge.sent_at <= excluded.sent_at - 60`, email, purpose, digest, now.Unix(), expires.Unix())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrCooldown
	}
	return err
}

func (s *Store) ForgetCode(ctx context.Context, email, purpose string, digest []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM email_challenge WHERE email = ? AND purpose = ? AND digest = ?", email, purpose, digest)
	return err
}

type EmailVerification struct {
	Email, Purpose, Username, PasswordHash string
	CodeDigest, SessionDigest              []byte
	Now, SessionExpires                    time.Time
}

func checkEmailCode(ctx context.Context, tx *sql.Tx, email, purpose string, supplied []byte, now time.Time) (bool, error) {
	var digest []byte
	var failures int
	err := tx.QueryRowContext(ctx, `SELECT digest,failures FROM email_challenge
		WHERE email = ? AND purpose = ? AND used = 0 AND expires_at > ?`, email, purpose, now.Unix()).
		Scan(&digest, &failures)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && failures >= 5) {
		return false, ErrCode
	}
	if err != nil {
		return false, err
	}
	if subtle.ConstantTimeCompare(digest, supplied) == 1 {
		return true, nil
	}
	_, err = tx.ExecContext(ctx, "UPDATE email_challenge SET failures = failures + 1 WHERE email = ? AND purpose = ?", email, purpose)
	return false, err
}

func (s *Store) VerifyEmail(ctx context.Context, in EmailVerification) (Account, error) {
	var account Account
	var invalid bool
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		match, err := checkEmailCode(ctx, tx, in.Email, in.Purpose, in.CodeDigest, in.Now)
		if invalid = !match; err != nil || invalid {
			return err
		}
		switch in.Purpose {
		case "reset":
			account.Email = in.Email
			err = tx.QueryRowContext(ctx, `SELECT a.id,a.balance,a.username,
				EXISTS(SELECT 1 FROM github_identity g WHERE g.account_id=a.id)
				FROM account a WHERE a.email=? AND a.username IS NOT NULL`, in.Email).
				Scan(&account.ID, &account.Balance, &account.Name, &account.GitHubLinked)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrCode
			}
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE account SET password_hash=? WHERE id=?", in.PasswordHash, account.ID); err != nil {
				return err
			}
			if err := revokeAccountAccess(ctx, tx, account.ID, nil); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE email_challenge SET used=1
				WHERE (email=? AND purpose IN ('login','register')) OR purpose=?`, account.Email, "bind:"+account.ID); err != nil {
				return err
			}
		case "register":
			account, err = accountForEmail(ctx, tx, in.Email, in.Now)
			if err != nil {
				return err
			}
			if err = setCredentials(ctx, tx, account.ID, in.Username, in.PasswordHash); err != nil {
				return err
			}
			account.Name = in.Username
		default:
			return ErrCode
		}
		if _, err := tx.ExecContext(ctx, "UPDATE email_challenge SET used = 1 WHERE email = ? AND purpose = ?", in.Email, in.Purpose); err != nil {
			return err
		}
		return issueSignedInSession(ctx, tx, account.ID, in.SessionDigest, in.Now, in.SessionExpires)
	})
	if err == nil && invalid {
		err = ErrCode
	}
	return account, err
}

type EmailBinding struct {
	AccountID, Email, Purpose, PasswordHash string
	CodeDigest, SessionDigest               []byte
	Now                                     time.Time
}

// BindEmail attaches the first verified email to an existing password account.
// It leaves the account ID, session and wallet in place.
func (s *Store) BindEmail(ctx context.Context, in EmailBinding) (Account, error) {
	var account Account
	var invalid bool
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		account, err = sessionAccount(ctx, tx, in.SessionDigest, in.Now)
		if err != nil {
			return err
		}
		if account.ID != in.AccountID || account.Email != "" {
			return ErrConflict
		}
		match, err := checkEmailCode(ctx, tx, in.Email, in.Purpose, in.CodeDigest, in.Now)
		if invalid = !match; err != nil || invalid {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE account SET email = ?
			WHERE id = ? AND email IS NULL AND password_hash = ?
			AND NOT EXISTS (SELECT 1 FROM account WHERE email = ?)`, in.Email, account.ID, in.PasswordHash, in.Email)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE email_challenge SET used = 1 WHERE email = ? AND purpose = ?", in.Email, in.Purpose); err != nil {
			return err
		}
		account.Email = in.Email
		return nil
	})
	if err == nil && invalid {
		err = ErrCode
	}
	return account, err
}

func (s *Store) PasswordAccount(ctx context.Context, username string) (Account, string, error) {
	var account Account
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT a.id,COALESCE(a.email,''),a.balance,a.username,a.password_hash,
		EXISTS(SELECT 1 FROM github_identity g WHERE g.account_id=a.id)
		FROM account a WHERE a.username=? AND a.password_hash IS NOT NULL`, username).
		Scan(&account.ID, &account.Email, &account.Balance, &account.Name, &hash, &account.GitHubLinked)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return account, hash, err
}

func (s *Store) PutSession(ctx context.Context, accountID string, digest []byte, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO account_session VALUES(?,?,?)", digest, accountID, expires.Unix())
	return err
}

// PutPasswordSession issues a session only while the verified password is current.
func (s *Store) PutPasswordSession(ctx context.Context, accountID, verifiedHash string, digest []byte, now, expires time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO account_session(digest,account_id,expires_at)
			SELECT ?,id,? FROM account WHERE id=? AND password_hash=?`, digest, expires.Unix(), accountID, verifiedHash)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		return recordLoginDay(ctx, tx, accountID, now)
	})
}

func (s *Store) SessionAccount(ctx context.Context, digest []byte, now time.Time) (Account, error) {
	return sessionAccount(ctx, s.db, digest, now)
}

func sessionAccount(ctx context.Context, db queryer, digest []byte, now time.Time) (Account, error) {
	var account Account
	err := db.QueryRowContext(ctx, `SELECT a.id,COALESCE(a.email,''),a.balance,COALESCE(a.username,''),
		EXISTS(SELECT 1 FROM github_identity g WHERE g.account_id=a.id)
		FROM account_session s JOIN account a ON a.id=s.account_id WHERE s.digest=? AND s.expires_at>?`, digest, now.Unix()).
		Scan(&account.ID, &account.Email, &account.Balance, &account.Name, &account.GitHubLinked)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return account, err
}

func (s *Store) RotateSession(ctx context.Context, oldDigest, newDigest []byte, now, expires time.Time) (Account, error) {
	var account Account
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		account, err = sessionAccount(ctx, tx, oldDigest, now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM account_session WHERE digest = ?", oldDigest); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO account_session VALUES(?,?,?)", newDigest, account.ID, expires.Unix())
		return err
	})
	return account, err
}

func (s *Store) RevokeSession(ctx context.Context, digest []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM account_session WHERE digest = ?", digest)
	return err
}

// revokeAccountAccess invalidates old sessions and GitHub handoffs that
// already belong to the account. A nil keepDigest revokes every user session.
func revokeAccountAccess(ctx context.Context, tx *sql.Tx, accountID string, keepDigest []byte) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM github_flow WHERE owner=? OR account_id=?", accountID, accountID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM admin_session WHERE account_id=?", accountID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM account_session WHERE account_id=? AND digest IS NOT ?", accountID, keepDigest)
	return err
}
