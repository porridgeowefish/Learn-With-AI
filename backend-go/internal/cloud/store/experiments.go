package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const experimentSchema = `
CREATE TABLE prompt_exposure (
  account_id TEXT NOT NULL REFERENCES account(id),
  exposure_id TEXT NOT NULL,
  experiment TEXT NOT NULL,
  variant TEXT NOT NULL,
  version TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(account_id,exposure_id,experiment)
);
CREATE INDEX prompt_exposure_date ON prompt_exposure(created_at);
PRAGMA user_version = 6;
`

func (s *Store) migrateExperimentRequests(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 13 {
			return err
		}
		_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS prompt_inference (
  account_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  experiment TEXT NOT NULL,
  variant TEXT NOT NULL,
  version TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  completed INTEGER NOT NULL CHECK(completed IN (0,1)),
  rate_limited_attempts INTEGER NOT NULL CHECK(rate_limited_attempts >= 0),
  elapsed_ms INTEGER NOT NULL CHECK(elapsed_ms >= 0),
  PRIMARY KEY(account_id,request_id),
  FOREIGN KEY(account_id,request_id) REFERENCES inference_request(account_id,request_id)
);
CREATE INDEX IF NOT EXISTS prompt_inference_date ON prompt_inference(created_at);
PRAGMA user_version = 13;`)
		return err
	})
}

type ExperimentUse struct {
	ExposureID, Experiment, Variant, Version string
	Completed, Teaching                      bool
	Elapsed                                  time.Duration
}

func recordExperimentUse(ctx context.Context, tx *sql.Tx, account, request string, at time.Time, use ExperimentUse, rateLimited int) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO prompt_inference(account_id,request_id,experiment,variant,version,created_at,completed,rate_limited_attempts,elapsed_ms)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
		account, request, use.Experiment, use.Variant, use.Version, at.Unix(), use.Completed, rateLimited, use.Elapsed.Milliseconds())
	if err != nil || !use.Completed {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO prompt_exposure(account_id,exposure_id,experiment,variant,version,created_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, account, use.ExposureID, use.Experiment, use.Variant, use.Version, at.Unix())
	if err != nil || !use.Teaching {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO prompt_teaching_rating(account_id,exposure_id,experiment)
		VALUES(?,?,?) ON CONFLICT DO NOTHING`, account, use.ExposureID, use.Experiment)
	return err
}

type ExperimentMetric struct {
	Experiment, Variant, Version                                    string
	Exposures, Users                                                int64
	Requests, Completed, Failed, RateLimitedAttempts, ObservedUsers *int64
	MeanLatencyMillis                                               *float64
	FirstObservedAt                                                 *time.Time
	Ratings                                                         int64
	MeanRating                                                      *float64
}

func (s *Store) ExperimentMetrics(ctx context.Context, since time.Time) ([]ExperimentMetric, error) {
	rows, err := s.db.QueryContext(ctx, `WITH exposures AS (
		SELECT e.experiment,e.variant,e.version,COUNT(*) AS exposures,COUNT(DISTINCT e.account_id) AS users,
			COUNT(r.stars) AS ratings,AVG(r.stars) AS rating
		FROM prompt_exposure e LEFT JOIN prompt_teaching_rating r USING(account_id,exposure_id,experiment)
		WHERE e.created_at >= ? GROUP BY e.experiment,e.variant,e.version
	), outcomes AS (
		SELECT experiment,variant,version,COUNT(*) AS requests,SUM(completed) AS completed,
			SUM(rate_limited_attempts) AS limited,COUNT(DISTINCT account_id) AS users,
			AVG(elapsed_ms) AS latency,MIN(created_at) AS first_seen
		FROM prompt_inference WHERE created_at >= ? GROUP BY experiment,variant,version
	), versions AS (
		SELECT experiment,variant,version FROM exposures UNION SELECT experiment,variant,version FROM outcomes
	)
	SELECT v.experiment,v.variant,v.version,COALESCE(e.exposures,0),COALESCE(e.users,0),
		o.requests,o.completed,o.requests-o.completed,o.limited,o.users,o.latency,o.first_seen,COALESCE(e.ratings,0),e.rating
	FROM versions v LEFT JOIN exposures e USING(experiment,variant,version)
	LEFT JOIN outcomes o USING(experiment,variant,version) ORDER BY v.experiment,v.variant,v.version`, since.Unix(), since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExperimentMetric{}
	for rows.Next() {
		var metric ExperimentMetric
		var firstSeen *int64
		if err := rows.Scan(&metric.Experiment, &metric.Variant, &metric.Version, &metric.Exposures, &metric.Users,
			&metric.Requests, &metric.Completed, &metric.Failed, &metric.RateLimitedAttempts, &metric.ObservedUsers,
			&metric.MeanLatencyMillis, &firstSeen, &metric.Ratings, &metric.MeanRating); err != nil {
			return nil, err
		}
		if firstSeen != nil {
			first := time.Unix(*firstSeen, 0).UTC()
			metric.FirstObservedAt = &first
		}
		result = append(result, metric)
	}
	return result, rows.Err()
}

type ExperimentDailyMetric struct {
	DailyMetric
	Experiment, Variant, Version string
	MissingCost                  int64
}

type ExperimentCohort struct {
	ControlOnly   int64 `json:"controlOnly"`
	TreatmentOnly int64 `json:"treatmentOnly"`
	Both          int64 `json:"both"`
}

func (s *Store) ExperimentCohorts(ctx context.Context, since time.Time, ids []string) (map[string]ExperimentCohort, error) {
	keys, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `WITH accounts AS (
		SELECT experiment,account_id,MAX(variant='control') AS a,MAX(variant='treatment') AS b
		FROM prompt_inference WHERE created_at >= ? AND experiment IN (SELECT value FROM json_each(?))
		GROUP BY experiment,account_id
	) SELECT experiment,SUM(a AND NOT b),SUM(b AND NOT a),SUM(a AND b) FROM accounts GROUP BY experiment`, since.Unix(), string(keys))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cohorts := make(map[string]ExperimentCohort, len(ids))
	for rows.Next() {
		var experiment string
		var counts ExperimentCohort
		if err := rows.Scan(&experiment, &counts.ControlOnly, &counts.TreatmentOnly, &counts.Both); err != nil {
			return nil, err
		}
		cohorts[experiment] = counts
	}
	return cohorts, rows.Err()
}

func (s *Store) ExperimentDailyUsage(ctx context.Context, now time.Time, days int) ([]ExperimentDailyMetric, error) {
	now = now.In(shanghai)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shanghai)
	start := today.AddDate(0, 0, 1-min(90, max(1, days)))
	until := today.AddDate(0, 0, 1)
	rows, err := s.db.QueryContext(ctx, `SELECT DATE(p.created_at,'unixepoch','+8 hours') AS day,
		p.experiment,p.variant,p.version,a.model,a.currency,
		COUNT(*),SUM(a.status='completed'),SUM(a.status='rate_limited'),
		SUM(a.status NOT IN ('rate_limited','auth_rejected') AND (a.input_tokens IS NULL OR a.output_tokens IS NULL)),
		SUM(a.status NOT IN ('rate_limited','auth_rejected') AND a.cost_nanos IS NULL),
		COALESCE(SUM(a.input_tokens),0),COALESCE(SUM(a.output_tokens),0),
		COALESCE(SUM(a.cache_read_tokens),0),COALESCE(SUM(a.cache_write_tokens),0),SUM(a.elapsed_ms),SUM(a.cost_nanos)
		FROM prompt_inference p JOIN inference_attempt a USING(account_id,request_id)
		WHERE p.created_at >= ? AND p.created_at < ?
		GROUP BY day,p.experiment,p.variant,p.version,a.model,a.currency
		ORDER BY day DESC,p.experiment,p.variant,p.version,a.model,a.currency`, start.Unix(), until.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ExperimentDailyMetric{}
	for rows.Next() {
		var row ExperimentDailyMetric
		if err := rows.Scan(&row.Day, &row.Experiment, &row.Variant, &row.Version, &row.Model, &row.Currency,
			&row.Attempts, &row.Completed, &row.RateLimited, &row.MissingUsage, &row.MissingCost,
			&row.Input, &row.Output, &row.Read, &row.Written, &row.LatencyMillis, &row.CostNanos); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
