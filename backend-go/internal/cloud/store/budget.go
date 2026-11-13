package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrBudget = errors.New("global inference cost budget exhausted")

const budgetSchema = `
CREATE TABLE cost_budget (
  account_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  month TEXT NOT NULL,
  currency TEXT NOT NULL,
  estimated INTEGER NOT NULL CHECK(estimated > 0),
  settled INTEGER CHECK(settled >= 0),
  PRIMARY KEY(account_id,request_id),
  FOREIGN KEY(account_id,request_id) REFERENCES inference_request(account_id,request_id)
);
CREATE INDEX cost_budget_period ON cost_budget(currency,month);
PRAGMA user_version = 8;
`

type BudgetHold struct {
	Currency         string
	Limit, Estimated int64
}

type BudgetUsage struct {
	Month    string `json:"month"`
	Currency string `json:"currency"`
	Reserved int64  `json:"reserved"`
	Settled  int64  `json:"settled"`
}

func (s *Store) BudgetUsage(ctx context.Context, currency string, now time.Time) (BudgetUsage, error) {
	return budgetUsage(ctx, s.db, currency, now)
}

func budgetUsage(ctx context.Context, db queryer, currency string, now time.Time) (BudgetUsage, error) {
	u := BudgetUsage{Month: now.In(shanghai).Format("2006-01"), Currency: currency}
	err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN settled IS NULL THEN estimated ELSE 0 END),0),
		COALESCE(SUM(settled),0) FROM (
		SELECT estimated,settled FROM cost_budget WHERE currency=? AND month=?
		UNION ALL SELECT estimated,settled FROM answer_comparison_arm WHERE currency=? AND month=?
		)`, currency, u.Month, currency, u.Month).Scan(&u.Reserved, &u.Settled)
	return u, err
}

func reserveCost(ctx context.Context, tx *sql.Tx, account, request string, now time.Time, hold BudgetHold) error {
	if hold.Currency == "" || hold.Estimated <= 0 || hold.Limit <= 0 {
		return ErrBudget
	}
	u, err := budgetUsage(ctx, tx, hold.Currency, now)
	if err != nil {
		return err
	}
	if u.Settled >= hold.Limit || u.Reserved >= hold.Limit-u.Settled || hold.Estimated > hold.Limit-u.Settled-u.Reserved {
		return ErrBudget
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO cost_budget(account_id,request_id,month,currency,estimated) VALUES(?,?,?,?,?)",
		account, request, u.Month, u.Currency, hold.Estimated)
	return err
}
