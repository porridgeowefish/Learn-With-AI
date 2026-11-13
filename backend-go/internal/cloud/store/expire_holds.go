package store

import (
	"context"
	"database/sql"
	"time"
)

// ExpireInferenceHolds releases unconfirmed learner charges after requests can
// no longer be running. The provider budget retains its estimate for unknown
// costs. Provider usage metrics keep their original nullable cost fields.
func (s *Store) ExpireInferenceHolds(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-10 * time.Minute).Unix()
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE inference_request
			SET state='settled', held=0, updated_at=?
			WHERE state IN ('reserved','pending') AND updated_at<=?`, now.Unix(), cutoff); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE cost_budget AS b SET settled=MAX(estimated, COALESCE((
			SELECT SUM(a.cost_nanos) FROM inference_attempt a WHERE a.account_id=b.account_id AND a.request_id=b.request_id AND a.currency=b.currency
		),0)) WHERE settled IS NULL AND EXISTS (
			SELECT 1 FROM inference_request r WHERE r.account_id=b.account_id AND r.request_id=b.request_id
			AND r.state='settled' AND (r.updated_at<=? OR r.updated_at=?))`, cutoff, now.Unix()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE answer_comparison_arm SET
			state=CASE WHEN state='running' THEN 'failed' ELSE state END,
			finished_at=COALESCE(finished_at,?),
			settled=CASE WHEN estimated>0 AND settled IS NULL THEN MAX(estimated,known_cost_floor) ELSE settled END
			WHERE created_at<=? AND (state='running' OR (estimated>0 AND settled IS NULL))`, now.Unix(), cutoff)
		return err
	})
}
