package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const reconcileSchema = `
CREATE TABLE inference_reconciliation (
  account_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  actor_id TEXT NOT NULL REFERENCES account(id),
  units INTEGER NOT NULL CHECK(units >= 0),
  cost INTEGER NOT NULL CHECK(cost >= 0),
  reason TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(account_id,request_id),
  FOREIGN KEY(account_id,request_id) REFERENCES inference_request(account_id,request_id)
);
PRAGMA user_version = 10;
`

type PendingInference struct {
	AccountID     string `json:"accountId"`
	RequestID     string `json:"requestId"`
	State         string `json:"state"`
	Held          int64  `json:"held"`
	Charged       int64  `json:"charged"`
	Currency      string `json:"currency"`
	Estimated     int64  `json:"estimated"`
	SettledCost   *int64 `json:"settledCost"`
	Model         string `json:"model"`
	RouteRevision string `json:"routeRevision"`
	CreatedAt     int64  `json:"createdAt"`
}

type ReconciliationReceipt struct {
	AccountID     string `json:"accountId"`
	RequestID     string `json:"requestId"`
	ActorID       string `json:"actorId"`
	Units         int64  `json:"units"`
	Currency      string `json:"currency"`
	CostNanos     *int64 `json:"costNanos"`
	Reason        string `json:"reason"`
	Model         string `json:"model"`
	RouteRevision string `json:"routeRevision"`
	CreatedAt     int64  `json:"createdAt"`
}

type ReconciliationPage struct {
	Receipts []ReconciliationReceipt `json:"receipts"`
	HasMore  bool                    `json:"hasMore"`
}

// ReconciliationHistory keeps confirmed request-level amounts separate from per-attempt metrics.
func (s *Store) ReconciliationHistory(ctx context.Context, account, request string, offset int) (ReconciliationPage, error) {
	const pageSize = 25
	rows, err := s.db.QueryContext(ctx, `SELECT r.account_id,r.request_id,r.actor_id,r.units,
		COALESCE(b.currency,''),CASE WHEN b.currency IS NOT NULL THEN r.cost END,r.reason,
		COALESCE((SELECT a.model FROM inference_attempt a WHERE a.account_id=r.account_id AND a.request_id=r.request_id
			ORDER BY a.ordinal LIMIT 1),''),COALESCE(route.revision,''),r.created_at
		FROM inference_reconciliation r LEFT JOIN cost_budget b USING(account_id,request_id)
		LEFT JOIN inference_route route USING(account_id,request_id)
		WHERE (?='' OR r.account_id=?) AND (?='' OR r.request_id=?)
		ORDER BY r.rowid DESC LIMIT ? OFFSET ?`, account, account, request, request, pageSize+1, offset)
	if err != nil {
		return ReconciliationPage{}, err
	}
	defer rows.Close()
	page := ReconciliationPage{Receipts: []ReconciliationReceipt{}}
	for rows.Next() {
		if len(page.Receipts) == pageSize {
			page.HasMore = true
			break
		}
		var receipt ReconciliationReceipt
		if err := rows.Scan(&receipt.AccountID, &receipt.RequestID, &receipt.ActorID, &receipt.Units, &receipt.Currency,
			&receipt.CostNanos, &receipt.Reason, &receipt.Model, &receipt.RouteRevision, &receipt.CreatedAt); err != nil {
			return ReconciliationPage{}, err
		}
		page.Receipts = append(page.Receipts, receipt)
	}
	return page, rows.Err()
}

const reconciliable = `(r.state = 'pending' OR (r.state = 'reserved' AND r.updated_at <= ?)
	OR (r.state = 'settled' AND b.settled IS NULL AND b.estimated IS NOT NULL))`

func (s *Store) PendingInference(ctx context.Context, now time.Time) ([]PendingInference, error) {
	rows, err := s.db.QueryContext(ctx, `WITH pending AS (
		SELECT r.account_id,r.request_id,r.state,r.held,r.charged,
			COALESCE(b.currency,'') AS currency,COALESCE(b.estimated,0) AS estimated,b.settled,r.created_at
		FROM inference_request r LEFT JOIN cost_budget b USING(account_id,request_id)
		WHERE `+reconciliable+` ORDER BY r.created_at,r.account_id,r.request_id LIMIT 200
	)
	SELECT p.*,COALESCE((SELECT a.model FROM inference_attempt a WHERE a.account_id=p.account_id AND a.request_id=p.request_id
		ORDER BY a.ordinal LIMIT 1),''),COALESCE(route.revision,'')
	FROM pending p LEFT JOIN inference_route route USING(account_id,request_id)
	ORDER BY p.created_at,p.account_id,p.request_id`, now.Add(-10*time.Minute).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingInference{}
	for rows.Next() {
		var item PendingInference
		if err := rows.Scan(&item.AccountID, &item.RequestID, &item.State, &item.Held, &item.Charged, &item.Currency,
			&item.Estimated, &item.SettledCost, &item.CreatedAt, &item.Model, &item.RouteRevision); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ReconcileInference(ctx context.Context, actor, account, request, reason string, units, cost int64, now time.Time) error {
	if reason == "" || units < 0 || cost < 0 {
		return ErrConflict
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var wasUnits, wasCost int64
		var wasReason string
		err := tx.QueryRowContext(ctx, "SELECT units,cost,reason FROM inference_reconciliation WHERE account_id=? AND request_id=?", account, request).
			Scan(&wasUnits, &wasCost, &wasReason)
		if err == nil {
			if wasUnits == units && wasCost == cost && wasReason == reason {
				return nil
			}
			return ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var ok bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inference_request r LEFT JOIN cost_budget b USING(account_id,request_id)
			WHERE r.account_id=? AND r.request_id=? AND `+reconciliable+`)`, account, request, now.Add(-10*time.Minute).Unix()).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrConflict
		}
		var known sql.NullInt64
		err = tx.QueryRowContext(ctx, "SELECT settled FROM cost_budget WHERE account_id=? AND request_id=?", account, request).Scan(&known)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) && cost != 0 {
			return ErrConflict
		}
		if known.Valid && known.Int64 != cost {
			return ErrConflict
		}
		if err := settleInference(ctx, tx, account, request, units, now, &cost); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO inference_reconciliation VALUES(?,?,?,?,?,?,?)", account, request, actor, units, cost, reason, now.Unix())
		return err
	})
}
