package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"
)

func (s *Store) SearchAccounts(ctx context.Context, keyword string) ([]Account, error) {
	page, err := s.ListAccounts(ctx, keyword, 0)
	if err != nil {
		return nil, err
	}
	result := make([]Account, 0, len(page.Accounts))
	for _, account := range page.Accounts {
		result = append(result, account.Account)
	}
	return result, nil
}

type ManagedAccount struct {
	Account
	CreatedAt        time.Time `json:"createdAt"`
	DailyCreditLimit int64     `json:"dailyCreditLimit"`
}

type AccountPage struct {
	Accounts []ManagedAccount `json:"accounts"`
	HasMore  bool             `json:"hasMore"`
}

// ListAccounts returns one bounded page without exposing authentication data.
func (s *Store) ListAccounts(ctx context.Context, keyword string, offset int) (AccountPage, error) {
	const pageSize = 25
	prefix := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(strings.TrimSpace(keyword))) + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(email,''), COALESCE(username,''), balance, created_at,
		COALESCE((SELECT units FROM account_credit_limit_change WHERE account_id=account.id ORDER BY id DESC LIMIT 1),0)
		FROM account
		WHERE id LIKE ? ESCAPE '\' OR username LIKE ? ESCAPE '\' OR email LIKE ? ESCAPE '\'
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, prefix, prefix, prefix, pageSize+1, offset)
	if err != nil {
		return AccountPage{}, err
	}
	defer rows.Close()
	result := AccountPage{Accounts: []ManagedAccount{}}
	for rows.Next() {
		if len(result.Accounts) == pageSize {
			result.HasMore = true
			break
		}
		var account ManagedAccount
		var createdAt int64
		if err := rows.Scan(&account.ID, &account.Email, &account.Name, &account.Balance, &createdAt, &account.DailyCreditLimit); err != nil {
			return AccountPage{}, err
		}
		account.CreatedAt = time.Unix(createdAt, 0).UTC()
		result.Accounts = append(result.Accounts, account)
	}
	return result, rows.Err()
}

const adminSchema = `
CREATE TABLE admin_factor (
  account_id TEXT PRIMARY KEY REFERENCES account(id),
  last_counter INTEGER NOT NULL
);
CREATE TABLE admin_session (
  digest BLOB PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES account(id),
  expires_at INTEGER NOT NULL
);
CREATE TABLE admin_adjustment (
  actor_id TEXT NOT NULL REFERENCES account(id),
  reference TEXT NOT NULL,
  account_id TEXT NOT NULL REFERENCES account(id),
  amount INTEGER NOT NULL,
  reason TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(actor_id, reference)
);
PRAGMA user_version = 5;
`

// PutAdminSession records the one-time factor counter when the operator uses a factor.
// A zero counter means the operator authenticated with the base session alone.
func (s *Store) PutAdminSession(ctx context.Context, account string, counter int64, baseDigest, digest []byte, now, expires time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		user, err := sessionAccount(ctx, tx, baseDigest, now)
		if err != nil {
			return err
		}
		if user.ID != account {
			return ErrNotFound
		}
		if counter != 0 {
			result, err := tx.ExecContext(ctx, `INSERT INTO admin_factor VALUES(?,?)
				ON CONFLICT(account_id) DO UPDATE SET last_counter=excluded.last_counter WHERE excluded.last_counter > admin_factor.last_counter`, account, counter)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrCode
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM admin_session WHERE expires_at <= ?", now.Unix()); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO admin_session VALUES(?,?,?)", digest, account, expires.Unix())
		return err
	})
}

func (s *Store) AdminAccount(ctx context.Context, digest []byte, now time.Time) (Account, error) {
	var user Account
	err := s.db.QueryRowContext(ctx, `SELECT a.id,COALESCE(a.email,'') FROM admin_session s JOIN account a ON a.id=s.account_id
		WHERE s.digest=? AND s.expires_at>?`, digest, now.Unix()).Scan(&user.ID, &user.Email)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return user, err
}

func (s *Store) RevokeAdminSession(ctx context.Context, digest []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM admin_session WHERE digest=?", digest)
	return err
}

func (s *Store) AdjustCredits(ctx context.Context, actorID, targetID, reference, reason string, amount int64, now time.Time) (Wallet, error) {
	var result Wallet
	if amount == 0 || amount == math.MinInt64 || reason == "" || reference == "" {
		return result, ErrConflict
	}
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = wallet(ctx, tx, targetID)
		if err != nil {
			return err
		}
		var previousTarget, previousReason string
		var previousAmount int64
		err = tx.QueryRowContext(ctx, "SELECT account_id,amount,reason FROM admin_adjustment WHERE actor_id=? AND reference=?",
			actorID, reference).Scan(&previousTarget, &previousAmount, &previousReason)
		if err == nil {
			if previousTarget != targetID || previousAmount != amount || previousReason != reason {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if amount < -(result.Balance-result.Held) || amount > math.MaxInt64-result.Balance {
			return ErrBalance
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO admin_adjustment VALUES(?,?,?,?,?,?)", actorID, reference, targetID, amount, reason, now.Unix()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO credit_movement VALUES(?,?,?,?,?)", targetID, "adjustment", actorID+":"+reference, amount, now.Unix()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE account SET balance = balance + ? WHERE id=?", amount, targetID); err != nil {
			return err
		}
		result.Balance += amount
		return nil
	})
	return result, err
}
