package store

import (
	"context"
	"database/sql"
	"time"
)

const metricsSchema = `
CREATE TABLE inference_attempt (
  account_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  ordinal INTEGER NOT NULL,
  model TEXT NOT NULL,
  connection_id TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  elapsed_ms INTEGER NOT NULL,
  input_tokens INTEGER,
  output_tokens INTEGER,
  cache_read_tokens INTEGER,
  cache_write_tokens INTEGER,
  currency TEXT NOT NULL,
  cost_nanos INTEGER,
  PRIMARY KEY(account_id,request_id,ordinal),
  FOREIGN KEY(account_id,request_id) REFERENCES inference_request(account_id,request_id)
);
CREATE INDEX inference_attempt_dates ON inference_attempt(created_at,status,account_id);
PRAGMA user_version = 4;
`

type AttemptMetric struct {
	ConnectionID, Status         string
	Elapsed                      time.Duration
	Input, Output, Read, Written *int
	Currency                     string
	CostNanos                    *int64
}

func (s *Store) RecordAttempts(ctx context.Context, accountID, requestID, model, revision string, at time.Time, attempts []AttemptMetric) error {
	if len(attempts) == 0 {
		return nil
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if revision != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO inference_route(account_id,request_id,revision) VALUES(?,?,?)
				ON CONFLICT(account_id,request_id) DO NOTHING`, accountID, requestID, revision); err != nil {
				return err
			}
		}
		for i, a := range attempts {
			if _, err := tx.ExecContext(ctx, `INSERT INTO inference_attempt VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(account_id,request_id,ordinal) DO NOTHING`, accountID, requestID, i, model, a.ConnectionID,
				a.Status, at.Unix(), a.Elapsed.Milliseconds(), a.Input, a.Output, a.Read, a.Written, a.Currency, a.CostNanos); err != nil {
				return err
			}
		}
		return nil
	})
}

type DailyMetric struct {
	Day, Model, Currency                           string
	Attempts, Completed, RateLimited, MissingUsage int64
	Input, Output, Read, Written, LatencyMillis    int64
	CostNanos                                      *int64
}

type Metrics struct {
	DAU, MAU           int64
	LoginDAU, LoginMAU int64
	ThroughDay         string
	Daily              []DailyMetric
}

type ConnectionUsage struct {
	Connection, Currency                            string
	Attempts, Completed, RateLimited, Input, Output int64
	CostNanos                                       *int64
}

func (s *Store) ConnectionUsage(ctx context.Context, since time.Time) ([]ConnectionUsage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT connection_id,currency,COUNT(*),SUM(status='completed'),SUM(status='rate_limited'),
		COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0),SUM(cost_nanos)
		FROM inference_attempt WHERE created_at >= ? GROUP BY connection_id,currency ORDER BY connection_id,currency`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ConnectionUsage{}
	for rows.Next() {
		var row ConnectionUsage
		if err := rows.Scan(&row.Connection, &row.Currency, &row.Attempts, &row.Completed, &row.RateLimited, &row.Input, &row.Output, &row.CostNanos); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Store) Metrics(ctx context.Context, now time.Time, days int) (Metrics, error) {
	now = now.In(shanghai)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghai)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, shanghai)
	until := today.AddDate(0, 0, 1).Unix()
	days = min(90, max(1, days))
	out := Metrics{ThroughDay: today.Format("2006-01-02"), Daily: []DailyMetric{}}
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT CASE WHEN created_at >= ? THEN account_id END), COUNT(DISTINCT account_id)
			FROM inference_attempt WHERE status = 'completed' AND created_at >= ? AND created_at < ?`, today.Unix(), month.Unix(), until).
			Scan(&out.DAU, &out.MAU)
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT CASE WHEN day=? THEN account_id END), COUNT(DISTINCT account_id)
			FROM account_login_day WHERE day>=? AND day<=?`, out.ThroughDay, month.Format("2006-01-02"), out.ThroughDay).
			Scan(&out.LoginDAU, &out.LoginMAU); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT DATE(created_at,'unixepoch','+8 hours') AS day, model, currency,
			COUNT(*), SUM(status = 'completed'), SUM(status = 'rate_limited'),
			SUM(status NOT IN ('rate_limited','auth_rejected') AND (input_tokens IS NULL OR output_tokens IS NULL)),
			COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read_tokens),0),
			COALESCE(SUM(cache_write_tokens),0), SUM(elapsed_ms), SUM(cost_nanos)
			FROM inference_attempt WHERE created_at >= ? AND created_at < ? GROUP BY day,model,currency ORDER BY day,model,currency`,
			today.AddDate(0, 0, 1-days).Unix(), until)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row DailyMetric
			if err := rows.Scan(&row.Day, &row.Model, &row.Currency, &row.Attempts, &row.Completed, &row.RateLimited,
				&row.MissingUsage, &row.Input, &row.Output, &row.Read, &row.Written, &row.LatencyMillis, &row.CostNanos); err != nil {
				return err
			}
			out.Daily = append(out.Daily, row)
		}
		return rows.Err()
	})
	return out, err
}
