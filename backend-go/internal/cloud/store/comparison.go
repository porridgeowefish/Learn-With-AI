package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *Store) migrateAnswerComparison(ctx context.Context) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version >= 19 {
			return err
		}
		_, err := tx.ExecContext(ctx, `
DROP TABLE IF EXISTS prompt_teaching_rating;
DROP TABLE IF EXISTS prompt_inference;
DROP TABLE IF EXISTS prompt_exposure;
UPDATE prompt_config_version SET content=json_remove(content,'$.experiments')
	WHERE json_type(content,'$.experiments') IS NOT NULL;
UPDATE prompt_config_version SET content=json_set(content,'$."resources"."teaching/teacher"',
	REPLACE(json_extract(content,'$."resources"."teaching/teacher"'),
	'评分由应用的星级控件处理，教师不要求学习者在对话中输入分数。',''))
	WHERE json_type(content,'$."resources"."teaching/teacher"')='text';
CREATE TABLE IF NOT EXISTS answer_comparison (
  subject TEXT PRIMARY KEY,
  audience TEXT NOT NULL CHECK(audience IN ('guest','registered')),
  operation TEXT NOT NULL,
  id TEXT NOT NULL UNIQUE,
  experiment TEXT NOT NULL,
  model TEXT NOT NULL,
  decision TEXT NOT NULL CHECK(decision IN ('disabled','not_sampled','sampled')),
  created_at INTEGER NOT NULL,
  question_digest TEXT NOT NULL DEFAULT '',
  pack_a TEXT NOT NULL,
  pack_b TEXT NOT NULL,
  vote TEXT CHECK(vote IN ('a','b','tie')),
  voted_at INTEGER
);
CREATE INDEX IF NOT EXISTS answer_comparison_audit ON answer_comparison(created_at DESC);
CREATE TABLE IF NOT EXISTS answer_comparison_arm (
  comparison_id TEXT NOT NULL REFERENCES answer_comparison(id),
  arm TEXT NOT NULL CHECK(arm IN ('a','b')),
  state TEXT NOT NULL CHECK(state IN ('running','completed','failed')),
  created_at INTEGER NOT NULL,
  finished_at INTEGER,
  month TEXT NOT NULL,
  currency TEXT NOT NULL,
  estimated INTEGER NOT NULL CHECK(estimated >= 0),
  settled INTEGER CHECK(settled >= 0),
  PRIMARY KEY(comparison_id,arm)
);
CREATE INDEX IF NOT EXISTS answer_comparison_cost ON answer_comparison_arm(currency,month);
PRAGMA user_version = 19;`)
		return err
	})
}

type Comparison struct {
	ID         string          `json:"id"`
	Experiment string          `json:"experiment"`
	Model      string          `json:"model"`
	Decision   string          `json:"decision"`
	Audience   string          `json:"audience"`
	PackA      json.RawMessage `json:"-"`
	PackB      json.RawMessage `json:"-"`
}

func (s *Store) StartComparison(ctx context.Context, subject, operation string, next Comparison, now time.Time) (Comparison, error) {
	var result Comparison
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		var priorOperation string
		var packA, packB string
		err := tx.QueryRowContext(ctx, `SELECT operation,id,experiment,model,decision,audience,pack_a,pack_b
			FROM answer_comparison WHERE subject=?`, subject).Scan(&priorOperation, &result.ID, &result.Experiment,
			&result.Model, &result.Decision, &result.Audience, &packA, &packB)
		if err == nil {
			if priorOperation != operation {
				result = Comparison{}
			} else {
				result.PackA, result.PackB = json.RawMessage(packA), json.RawMessage(packB)
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		next.ID = randomID()
		_, err = tx.ExecContext(ctx, `INSERT INTO answer_comparison(subject,audience,operation,id,experiment,model,decision,created_at,pack_a,pack_b)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, subject, next.Audience, operation, next.ID, next.Experiment, next.Model, next.Decision,
			now.Unix(), string(next.PackA), string(next.PackB))
		result = next
		return err
	})
	return result, err
}

func (s *Store) ComparisonForSubject(ctx context.Context, subject, id string) (Comparison, error) {
	var result Comparison
	var packA, packB string
	err := s.db.QueryRowContext(ctx, `SELECT id,experiment,model,decision,audience,pack_a,pack_b
		FROM answer_comparison WHERE subject=? AND id=?`, subject, id).Scan(&result.ID, &result.Experiment, &result.Model, &result.Decision,
		&result.Audience, &packA, &packB)
	result.PackA, result.PackB = json.RawMessage(packA), json.RawMessage(packB)
	return result, err
}

func (s *Store) SelectedComparisonPrompt(ctx context.Context, subject string) (json.RawMessage, error) {
	var selected string
	err := s.db.QueryRowContext(ctx, `SELECT CASE vote WHEN 'b' THEN pack_b ELSE pack_a END
		FROM answer_comparison WHERE subject=? AND vote IS NOT NULL`, subject).Scan(&selected)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return json.RawMessage(selected), err
}

func (s *Store) ReserveComparisonArm(ctx context.Context, id, arm, questionDigest string, now time.Time, hold BudgetHold) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "UPDATE answer_comparison SET question_digest=? WHERE id=? AND (question_digest='' OR question_digest=?)", questionDigest, id, questionDigest)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return ErrConflict
		}
		if hold.Limit > 0 {
			usage, err := budgetUsage(ctx, tx, hold.Currency, now)
			if err != nil {
				return err
			}
			if hold.Estimated <= 0 || usage.Settled >= hold.Limit || usage.Reserved >= hold.Limit-usage.Settled ||
				hold.Estimated > hold.Limit-usage.Settled-usage.Reserved {
				return ErrBudget
			}
		}
		result, err = tx.ExecContext(ctx, `INSERT INTO answer_comparison_arm(comparison_id,arm,state,created_at,month,currency,estimated)
			VALUES(?,?,'running',?,?,?,?) ON CONFLICT DO NOTHING`, id, arm, now.Unix(), now.In(shanghai).Format("2006-01"), hold.Currency, hold.Estimated)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return ErrConflict
		}
		return nil
	})
}

func (s *Store) FinishComparisonArm(ctx context.Context, id, arm string, completed bool, cost *int64, now time.Time) error {
	state := "failed"
	if completed {
		state = "completed"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE answer_comparison_arm SET state=?,finished_at=?,settled=?
		WHERE comparison_id=? AND arm=? AND state='running'`, state, now.Unix(), cost, id, arm)
	return err
}

func (s *Store) VoteComparison(ctx context.Context, subject, id, vote string, now time.Time) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var current sql.NullString
		var completed int
		err := tx.QueryRowContext(ctx, `SELECT vote,(SELECT COUNT(*) FROM answer_comparison_arm a WHERE a.comparison_id=c.id AND a.state='completed')
			FROM answer_comparison c WHERE c.subject=? AND c.id=? AND c.decision='sampled'`, subject, id).Scan(&current, &completed)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if current.Valid && current.String == vote {
			return nil
		}
		if current.Valid || completed != 2 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, "UPDATE answer_comparison SET vote=?,voted_at=? WHERE id=?", vote, now.Unix(), id)
		return err
	})
}

type ComparisonMetric struct {
	Experiment string `json:"experiment"`
	Audience   string `json:"audience"`
	Eligible   int64  `json:"eligible"`
	Sampled    int64  `json:"sampled"`
	Completed  int64  `json:"completed"`
	A          int64  `json:"a"`
	B          int64  `json:"b"`
	Tie        int64  `json:"tie"`
}

func (s *Store) ComparisonMetrics(ctx context.Context) ([]ComparisonMetric, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT experiment,audience,COUNT(*),SUM(decision='sampled'),
		SUM((SELECT COUNT(*) FROM answer_comparison_arm a WHERE a.comparison_id=c.id AND a.state='completed')=2),
		COALESCE(SUM(vote='a'),0),COALESCE(SUM(vote='b'),0),COALESCE(SUM(vote='tie'),0)
		FROM answer_comparison c GROUP BY experiment,audience ORDER BY experiment,audience`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ComparisonMetric{}
	for rows.Next() {
		var m ComparisonMetric
		if err := rows.Scan(&m.Experiment, &m.Audience, &m.Eligible, &m.Sampled, &m.Completed, &m.A, &m.B, &m.Tie); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

type ComparisonAudit struct {
	ID         string `json:"id"`
	Experiment string `json:"experiment"`
	Audience   string `json:"audience"`
	Model      string `json:"model"`
	Decision   string `json:"decision"`
	A          string `json:"a"`
	B          string `json:"b"`
	Vote       string `json:"vote"`
	CreatedAt  int64  `json:"createdAt"`
	VotedAt    int64  `json:"votedAt"`
}

func (s *Store) ComparisonAudit(ctx context.Context, audience string, offset int) ([]ComparisonAudit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.experiment,c.audience,c.model,c.decision,
		COALESCE(a.state,''),COALESCE(b.state,''),COALESCE(c.vote,''),c.created_at,COALESCE(c.voted_at,0)
		FROM answer_comparison c LEFT JOIN answer_comparison_arm a ON a.comparison_id=c.id AND a.arm='a'
		LEFT JOIN answer_comparison_arm b ON b.comparison_id=c.id AND b.arm='b'
		WHERE (?='' OR c.audience=?) ORDER BY c.created_at DESC,c.id DESC LIMIT 50 OFFSET ?`, audience, audience, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ComparisonAudit{}
	for rows.Next() {
		var a ComparisonAudit
		if err := rows.Scan(&a.ID, &a.Experiment, &a.Audience, &a.Model, &a.Decision, &a.A, &a.B, &a.Vote, &a.CreatedAt, &a.VotedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
