package store

import (
	"context"
	"database/sql"
	"time"
)

func (s *Store) migrateInferenceRoutes(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 14 {
			return err
		}
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS inference_route (
  account_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  revision TEXT NOT NULL,
  PRIMARY KEY(account_id,request_id),
  FOREIGN KEY(account_id,request_id) REFERENCES inference_request(account_id,request_id)
);
PRAGMA user_version = 14;`)
		return err
	})
}

type RouteUsage struct {
	Revision, Model, Connection, Currency string
	Attempts, Completed, RateLimited      int64
	Input, Output, Read, Written          int64
	MissingCost                           int64
	CostNanos                             *int64
}

func (s *Store) RouteUsage(ctx context.Context, now time.Time, days int) ([]RouteUsage, error) {
	now = now.In(shanghai)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghai)
	start := today.AddDate(0, 0, 1-min(90, max(1, days)))
	until := today.AddDate(0, 0, 1)
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(r.revision,'') AS route_revision,
		a.model,a.connection_id,a.currency,COUNT(*),SUM(a.status='completed'),SUM(a.status='rate_limited'),
		COALESCE(SUM(a.input_tokens),0),COALESCE(SUM(a.output_tokens),0),
		COALESCE(SUM(a.cache_read_tokens),0),COALESCE(SUM(a.cache_write_tokens),0),
		SUM(a.status NOT IN ('rate_limited','auth_rejected') AND a.cost_nanos IS NULL),SUM(a.cost_nanos)
		FROM inference_attempt a LEFT JOIN inference_route r USING(account_id,request_id)
		WHERE a.created_at >= ? AND a.created_at < ?
		GROUP BY route_revision,a.model,a.connection_id,a.currency
		ORDER BY route_revision,a.model,a.connection_id,a.currency`, start.Unix(), until.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RouteUsage{}
	for rows.Next() {
		var row RouteUsage
		if err := rows.Scan(&row.Revision, &row.Model, &row.Connection, &row.Currency, &row.Attempts, &row.Completed,
			&row.RateLimited, &row.Input, &row.Output, &row.Read, &row.Written, &row.MissingCost, &row.CostNanos); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
