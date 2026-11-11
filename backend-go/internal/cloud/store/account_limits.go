package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var ErrDailyCreditLimit = errors.New("daily account credit limit reached")

const MaxDailyCreditLimit int64 = 1_000_000_000_000

func (s *Store) migrateAccountLimits(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 15 {
			return err
		}
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS account_credit_limit_change (
  id INTEGER PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES account(id),
  actor_id TEXT NOT NULL REFERENCES account(id),
  reference TEXT NOT NULL,
  units INTEGER NOT NULL CHECK (units >= 0),
  reason TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  UNIQUE(actor_id, reference)
);
CREATE INDEX IF NOT EXISTS account_credit_limit_latest ON account_credit_limit_change(account_id, id DESC);
CREATE INDEX IF NOT EXISTS inference_account_day ON inference_request(account_id, created_at);
PRAGMA user_version = 15;`)
		return err
	})
}

// SetDailyCreditLimit writes an audited override. Zero removes the limit.
func (s *Store) SetDailyCreditLimit(ctx context.Context, actor, account, reference, reason string, units int64, now time.Time) error {
	if units < 0 || units > MaxDailyCreditLimit || reference == "" || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return ErrConflict
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var target, previousReason string
		var previousUnits int64
		err := tx.QueryRowContext(ctx, `SELECT account_id,units,reason FROM account_credit_limit_change WHERE actor_id=? AND reference=?`,
			actor, reference).Scan(&target, &previousUnits, &previousReason)
		if err == nil {
			if target != account || previousUnits != units || previousReason != reason {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO account_credit_limit_change(account_id,actor_id,reference,units,reason,created_at)
			SELECT id,?,?,?,?,? FROM account WHERE id=?`, actor, reference, units, reason, now.Unix(), account)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err == nil && changed == 0 {
			return ErrNotFound
		}
		return err
	})
}

func checkDailyCreditLimit(ctx context.Context, tx *sql.Tx, account string, units int64, now time.Time) error {
	var limit int64
	err := tx.QueryRowContext(ctx, `SELECT units FROM account_credit_limit_change WHERE account_id=? ORDER BY id DESC LIMIT 1`, account).Scan(&limit)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && limit == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	local := now.In(shanghai)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghai)
	var used int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(charged + held),0) FROM inference_request
		WHERE account_id=? AND created_at>=? AND created_at<?`, account, start.Unix(), start.AddDate(0, 0, 1).Unix()).Scan(&used)
	if err != nil {
		return err
	}
	if units > limit-used {
		return ErrDailyCreditLimit
	}
	return nil
}
