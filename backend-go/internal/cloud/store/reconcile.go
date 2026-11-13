package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

func (s *Store) migrateComparisonReconciliation(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 21 {
			return err
		}
		// Some upgrade tests start with a newer database and rewind its version.
		var knownCostColumn bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pragma_table_info('answer_comparison_arm') WHERE name='known_cost_floor')`).Scan(&knownCostColumn); err != nil {
			return err
		}
		if !knownCostColumn {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE answer_comparison_arm ADD COLUMN known_cost_floor INTEGER NOT NULL DEFAULT 0 CHECK(known_cost_floor >= 0)`); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS comparison_cost_reconciliation (
			comparison_id TEXT NOT NULL,
			arm TEXT NOT NULL,
			actor_id TEXT NOT NULL REFERENCES account(id),
			cost INTEGER NOT NULL CHECK(cost >= 0),
			reason TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY(comparison_id,arm),
			FOREIGN KEY(comparison_id,arm) REFERENCES answer_comparison_arm(comparison_id,arm)
		);
		PRAGMA user_version = 21;`)
		return err
	})
}

type PendingInference struct {
	Kind           string `json:"kind,omitempty"`
	ComparisonID   string `json:"comparisonId,omitempty"`
	Arm            string `json:"arm,omitempty"`
	Experiment     string `json:"experiment,omitempty"`
	KnownCostFloor int64  `json:"knownCostFloor,omitempty"`
	AccountID      string `json:"accountId"`
	RequestID      string `json:"requestId"`
	State          string `json:"state"`
	Held           int64  `json:"held"`
	Charged        int64  `json:"charged"`
	Currency       string `json:"currency"`
	Estimated      int64  `json:"estimated"`
	SettledCost    *int64 `json:"settledCost"`
	Model          string `json:"model"`
	RouteRevision  string `json:"routeRevision"`
	CreatedAt      int64  `json:"createdAt"`
}

type ReconciliationReceipt struct {
	Kind          string `json:"kind,omitempty"`
	ComparisonID  string `json:"comparisonId,omitempty"`
	Arm           string `json:"arm,omitempty"`
	Experiment    string `json:"experiment,omitempty"`
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
func (s *Store) ReconciliationHistory(ctx context.Context, account, request, comparison string, offset int) (ReconciliationPage, error) {
	const pageSize = 25
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM (SELECT r.account_id,r.request_id,r.actor_id,r.units,
		COALESCE(b.currency,''),CASE WHEN b.currency IS NOT NULL THEN r.cost END,r.reason,
		COALESCE((SELECT a.model FROM inference_attempt a WHERE a.account_id=r.account_id AND a.request_id=r.request_id
			ORDER BY a.ordinal LIMIT 1),''),COALESCE(route.revision,''),r.created_at,'' AS kind,'' AS comparison_id,'' AS arm,'' AS experiment,r.rowid AS receipt_order
		FROM inference_reconciliation r LEFT JOIN cost_budget b USING(account_id,request_id)
		LEFT JOIN inference_route route USING(account_id,request_id)
		WHERE ?='' AND (?='' OR r.account_id=?) AND (?='' OR r.request_id=?)
		UNION ALL
		SELECT '','',r.actor_id,0,a.currency,r.cost,r.reason,c.model,'',r.created_at,'comparison',r.comparison_id,r.arm,c.experiment,r.rowid
		FROM comparison_cost_reconciliation r JOIN answer_comparison_arm a USING(comparison_id,arm) JOIN answer_comparison c ON c.id=r.comparison_id
		WHERE ?='' AND ?='' AND (?='' OR r.comparison_id=?))
		ORDER BY created_at DESC,kind,receipt_order DESC LIMIT ? OFFSET ?`, comparison, account, account, request, request,
		account, request, comparison, comparison, pageSize+1, offset)
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
		var receiptOrder int64
		if err := rows.Scan(&receipt.AccountID, &receipt.RequestID, &receipt.ActorID, &receipt.Units, &receipt.Currency,
			&receipt.CostNanos, &receipt.Reason, &receipt.Model, &receipt.RouteRevision, &receipt.CreatedAt, &receipt.Kind, &receipt.ComparisonID, &receipt.Arm, &receipt.Experiment, &receiptOrder); err != nil {
			return ReconciliationPage{}, err
		}
		page.Receipts = append(page.Receipts, receipt)
	}
	return page, rows.Err()
}

const reconciliable = `(r.state = 'pending' OR (r.state = 'reserved' AND r.updated_at <= ?)
	OR (r.state = 'settled' AND b.settled IS NULL AND b.estimated IS NOT NULL))`

const comparisonReconciliable = `a.estimated > 0 AND a.settled IS NULL AND (a.state <> 'running' OR a.created_at <= ?)`

func (s *Store) PendingInference(ctx context.Context, now time.Time) ([]PendingInference, error) {
	rows, err := s.db.QueryContext(ctx, `WITH pending AS (
		SELECT r.account_id,r.request_id,r.state,r.held,r.charged,
			COALESCE(b.currency,'') AS currency,COALESCE(b.estimated,0) AS estimated,b.settled,r.created_at,
			'' AS kind,'' AS comparison_id,'' AS arm,'' AS experiment,0 AS known_cost_floor
		FROM inference_request r LEFT JOIN cost_budget b USING(account_id,request_id)
		WHERE `+reconciliable+`
		UNION ALL
		SELECT '','',CASE a.state WHEN 'running' THEN 'reserved' ELSE 'settled' END,0,0,a.currency,a.estimated,a.settled,a.created_at,
			'comparison',a.comparison_id,a.arm,c.experiment,a.known_cost_floor
		FROM answer_comparison_arm a JOIN answer_comparison c ON c.id=a.comparison_id WHERE `+comparisonReconciliable+`
		ORDER BY created_at,account_id,request_id,comparison_id,arm LIMIT 200
	)
	SELECT p.*,COALESCE(c.model,(SELECT a.model FROM inference_attempt a WHERE a.account_id=p.account_id AND a.request_id=p.request_id
		ORDER BY a.ordinal LIMIT 1),''),COALESCE(route.revision,'')
	FROM pending p LEFT JOIN inference_route route USING(account_id,request_id) LEFT JOIN answer_comparison c ON c.id=p.comparison_id
	ORDER BY p.created_at,p.account_id,p.request_id,p.comparison_id,p.arm`, now.Add(-10*time.Minute).Unix(), now.Add(-10*time.Minute).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingInference{}
	for rows.Next() {
		var item PendingInference
		if err := rows.Scan(&item.AccountID, &item.RequestID, &item.State, &item.Held, &item.Charged, &item.Currency,
			&item.Estimated, &item.SettledCost, &item.CreatedAt, &item.Kind, &item.ComparisonID, &item.Arm, &item.Experiment, &item.KnownCostFloor, &item.Model, &item.RouteRevision); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ReconcileComparisonCost(ctx context.Context, actor, id, arm, reason string, cost int64, now time.Time) error {
	if strings.TrimSpace(reason) == "" || cost < 0 || (arm != "a" && arm != "b") {
		return ErrConflict
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var previousCost int64
		var previousReason string
		err := tx.QueryRowContext(ctx, `SELECT cost,reason FROM comparison_cost_reconciliation WHERE comparison_id=? AND arm=?`, id, arm).Scan(&previousCost, &previousReason)
		if err == nil {
			if previousCost == cost && previousReason == reason {
				return nil
			}
			return ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		updated, err := tx.ExecContext(ctx, `UPDATE answer_comparison_arm AS a SET settled=?
			WHERE comparison_id=? AND arm=? AND known_cost_floor<=? AND `+comparisonReconciliable, cost, id, arm, cost, now.Add(-10*time.Minute).Unix())
		if err != nil {
			return err
		}
		if count, err := updated.RowsAffected(); err != nil || count != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO comparison_cost_reconciliation VALUES(?,?,?,?,?,?)`, id, arm, actor, cost, reason, now.Unix())
		return err
	})
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
