// apps/backend/internal/platform/price/job_repo.go
package price

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusResolved   JobStatus = "resolved"
	JobStatusFailed     JobStatus = "failed"
)

type BackfillJob struct {
	ID            uuid.UUID
	AssetID       uuid.UUID
	TargetTime    time.Time
	Status        JobStatus
	Attempts      int
	NextAttemptAt time.Time
	LockedAt      *time.Time
	LastError     string
	CreatedAt     time.Time
	ResolvedAt    *time.Time
}

// JobRepository is the port for the backfill queue.
type JobRepository interface {
	// Enqueue inserts a job or returns the existing one (idempotent on asset+time).
	Enqueue(ctx context.Context, assetID uuid.UUID, targetTime time.Time) (*BackfillJob, error)

	// ClaimReady attempts to lock one ready job atomically (FOR UPDATE SKIP LOCKED).
	// Returns (nil, nil) if none ready.
	ClaimReady(ctx context.Context) (*BackfillJob, error)

	// MarkResolved sets status=resolved and resolved_at.
	MarkResolved(ctx context.Context, jobID uuid.UUID) error

	// Reschedule increments attempts, sets next_attempt_at, updates last_error,
	// and may transition to status=failed when attempts >= MaxAttempts.
	Reschedule(ctx context.Context, jobID uuid.UUID, attempts int, nextAttemptAt time.Time, lastError string, terminal bool) error

	// UnlockWithoutCounting releases the lock without incrementing attempts
	// (used on rate-limit / transient errors).
	UnlockWithoutCounting(ctx context.Context, jobID uuid.UUID, nextAttemptAt time.Time) error

	// ReapStale resets status=in_progress rows whose lock is older than `staleAfter`.
	ReapStale(ctx context.Context, staleAfter time.Duration) (int, error)
}
