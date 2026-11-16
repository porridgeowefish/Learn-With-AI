package store

import (
	"context"
	"time"
)

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

// SupplierCost keeps the existing Backstage report shape. Reserved is always zero.
type SupplierCost struct {
	Month    string `json:"month"`
	Currency string `json:"currency"`
	Reserved int64  `json:"reserved"`
	Settled  int64  `json:"settled"`
}

func (s *Store) MonthlySupplierCost(ctx context.Context, currency string, now time.Time) (SupplierCost, error) {
	local := now.In(shanghai)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, shanghai)
	u := SupplierCost{Month: start.Format("2006-01"), Currency: currency}
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(cost),0) FROM (
		SELECT settled AS cost FROM cost_budget WHERE currency=? AND month=?
		UNION ALL SELECT a.cost_nanos FROM inference_attempt a WHERE a.currency=? AND a.created_at>=? AND a.created_at<?
			AND NOT EXISTS (SELECT 1 FROM cost_budget b WHERE b.account_id=a.account_id AND b.request_id=a.request_id AND b.currency=a.currency)
		UNION ALL SELECT COALESCE(settled,known_cost_floor)
		FROM answer_comparison_arm WHERE currency=? AND month=?
		)`, currency, u.Month, currency, start.Unix(), start.AddDate(0, 1, 0).Unix(), currency, u.Month).Scan(&u.Settled)
	return u, err
}
