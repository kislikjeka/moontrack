package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/platform/price"
)

// PriceBackfillJobRepository is the postgres implementation of price.JobRepository.
type PriceBackfillJobRepository struct {
	pool *pgxpool.Pool
}

// NewPriceBackfillJobRepository creates a new PriceBackfillJobRepository.
func NewPriceBackfillJobRepository(pool *pgxpool.Pool) *PriceBackfillJobRepository {
	return &PriceBackfillJobRepository{pool: pool}
}

// Enqueue inserts a backfill job or returns the existing one (idempotent on asset+time).
// target_time is always truncated to minute precision before insert.
func (r *PriceBackfillJobRepository) Enqueue(ctx context.Context, assetID uuid.UUID, targetTime time.Time) (*price.BackfillJob, error) {
	target := targetTime.UTC().Truncate(time.Minute)
	row := r.pool.QueryRow(ctx, `
		INSERT INTO price_backfill_jobs (asset_id, target_time)
		VALUES ($1, $2)
		ON CONFLICT (asset_id, target_time) DO UPDATE
			SET asset_id = EXCLUDED.asset_id  -- no-op to trigger RETURNING
		RETURNING id, asset_id, target_time, status, attempts, next_attempt_at, locked_at, last_error, created_at, resolved_at
	`, assetID, target)
	return scanJob(row)
}

// ClaimReady atomically claims one ready job using FOR UPDATE SKIP LOCKED.
// Returns (nil, nil) if no job is ready.
func (r *PriceBackfillJobRepository) ClaimReady(ctx context.Context) (*price.BackfillJob, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	row := tx.QueryRow(ctx, `
		SELECT id, asset_id, target_time, status, attempts, next_attempt_at, locked_at, last_error, created_at, resolved_at
		FROM price_backfill_jobs
		WHERE status = 'pending' AND next_attempt_at <= NOW()
		ORDER BY next_attempt_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`)
	job, err := scanJob(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE price_backfill_jobs
		SET status = 'in_progress', locked_at = NOW()
		WHERE id = $1
	`, job.ID)
	if err != nil {
		return nil, err
	}

	job.Status = price.JobStatusInProgress
	now := time.Now().UTC()
	job.LockedAt = &now

	return job, tx.Commit(ctx)
}

// MarkResolved sets the job to resolved and records resolved_at.
func (r *PriceBackfillJobRepository) MarkResolved(ctx context.Context, jobID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE price_backfill_jobs
		SET status = 'resolved', resolved_at = NOW(), locked_at = NULL
		WHERE id = $1
	`, jobID)
	return err
}

// Reschedule updates attempts, next_attempt_at and last_error.
// When terminal=true, transitions status to failed instead of pending.
func (r *PriceBackfillJobRepository) Reschedule(ctx context.Context, jobID uuid.UUID, attempts int, next time.Time, lastError string, terminal bool) error {
	status := price.JobStatusPending
	if terminal {
		status = price.JobStatusFailed
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE price_backfill_jobs
		SET attempts = $2, next_attempt_at = $3, last_error = $4, status = $5, locked_at = NULL
		WHERE id = $1
	`, jobID, attempts, next, lastError, string(status))
	return err
}

// UnlockWithoutCounting releases the lock and reschedules without incrementing attempts.
// Used when a rate-limit or transient error should not consume a retry.
func (r *PriceBackfillJobRepository) UnlockWithoutCounting(ctx context.Context, jobID uuid.UUID, next time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE price_backfill_jobs
		SET status = 'pending', next_attempt_at = $2, locked_at = NULL
		WHERE id = $1
	`, jobID, next)
	return err
}

// ReapStale resets in_progress jobs whose locked_at is older than staleAfter back to pending.
// Returns the number of rows affected.
func (r *PriceBackfillJobRepository) ReapStale(ctx context.Context, staleAfter time.Duration) (int, error) {
	ct, err := r.pool.Exec(ctx, `
		UPDATE price_backfill_jobs
		SET status = 'pending', locked_at = NULL
		WHERE status = 'in_progress' AND locked_at < NOW() - $1::interval
	`, fmt.Sprintf("%d seconds", int(staleAfter.Seconds())))
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

// scanJob scans a pgx.Row into a BackfillJob.
func scanJob(row pgx.Row) (*price.BackfillJob, error) {
	var j price.BackfillJob
	var status string
	var locked, resolved *time.Time
	var lastErr *string
	err := row.Scan(
		&j.ID, &j.AssetID, &j.TargetTime, &status, &j.Attempts,
		&j.NextAttemptAt, &locked, &lastErr, &j.CreatedAt, &resolved,
	)
	if err != nil {
		return nil, err
	}
	j.Status = price.JobStatus(status)
	if locked != nil {
		j.LockedAt = locked
	}
	if lastErr != nil {
		j.LastError = *lastErr
	}
	if resolved != nil {
		j.ResolvedAt = resolved
	}
	return &j, nil
}
