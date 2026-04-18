//go:build integration

package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/stretchr/testify/require"
)

// TestBackfillWorker_ConcurrentClaims verifies that under real postgres semantics,
// two goroutines contending on the same ready job will have exactly one winner;
// the other must receive (nil, nil) thanks to FOR UPDATE SKIP LOCKED.
//
// Repeats the experiment 20 times to catch flakiness: if the locking is broken
// (e.g. missing SKIP LOCKED / missing transaction), both goroutines may succeed
// intermittently.
func TestBackfillWorker_ConcurrentClaims(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetJobQueue(t)
	repo := NewPriceBackfillJobRepository(testDB.Pool)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		// Seed one ready job per iteration.
		resetJobQueue(t)
		assetID := seedAsset(t)
		target := time.Now().UTC().Truncate(time.Minute)
		_, err := repo.Enqueue(ctx, assetID, target)
		require.NoError(t, err, "iteration %d: enqueue", i)

		var wg sync.WaitGroup
		results := make(chan *price.BackfillJob, 2)
		errs := make(chan error, 2)

		start := make(chan struct{})
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start // release both at once to maximize contention
				j, err := repo.ClaimReady(ctx)
				if err != nil {
					errs <- err
					return
				}
				results <- j
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)

		for err := range errs {
			require.NoError(t, err, "iteration %d: claim error", i)
		}

		winners := 0
		nils := 0
		for j := range results {
			if j != nil {
				winners++
			} else {
				nils++
			}
		}
		require.Equal(t, 1, winners, "iteration %d: exactly one goroutine must claim the job, got winners=%d nils=%d", i, winners, nils)
		require.Equal(t, 1, nils, "iteration %d: exactly one goroutine must receive nil, got winners=%d nils=%d", i, winners, nils)
	}
}

// TestReaper_ReclaimsOrphanedJobs verifies that when a worker dies mid-claim
// (job is stuck in_progress with an old locked_at), ReapStale transitions it
// back to pending so another worker can pick it up.
func TestReaper_ReclaimsOrphanedJobs(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetJobQueue(t)
	assetID := seedAsset(t)
	repo := NewPriceBackfillJobRepository(testDB.Pool)

	// Seed a job and claim it (simulating a worker that has taken the job).
	target := time.Now().UTC().Truncate(time.Minute)
	_, err := repo.Enqueue(ctx, assetID, target)
	require.NoError(t, err)

	claimed, err := repo.ClaimReady(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, price.JobStatusInProgress, claimed.Status)

	// Artificially age the lock to simulate an orphaned worker.
	_, err = testDB.Pool.Exec(ctx,
		`UPDATE price_backfill_jobs SET locked_at = NOW() - INTERVAL '20 minutes' WHERE id = $1`,
		claimed.ID)
	require.NoError(t, err)

	// Reap stale jobs (threshold 10 minutes — ours is 20 minutes old).
	n, err := repo.ReapStale(ctx, 10*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, n, "one stale job should have been reaped")

	// Verify the row is now pending and locked_at is NULL.
	var status string
	var lockedAt *time.Time
	err = testDB.Pool.QueryRow(ctx,
		`SELECT status, locked_at FROM price_backfill_jobs WHERE id = $1`,
		claimed.ID).Scan(&status, &lockedAt)
	require.NoError(t, err)
	require.Equal(t, string(price.JobStatusPending), status)
	require.Nil(t, lockedAt)

	// Confirm the reaped job is re-claimable.
	reclaimed, err := repo.ClaimReady(ctx)
	require.NoError(t, err)
	require.NotNil(t, reclaimed)
	require.Equal(t, claimed.ID, reclaimed.ID)
	require.Equal(t, price.JobStatusInProgress, reclaimed.Status)
}
