// apps/backend/internal/platform/price/backfill_worker_test.go
package price_test

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/stretchr/testify/require"
)

type memJobRepo struct {
	mu       sync.Mutex
	jobs     map[uuid.UUID]*price.BackfillJob
	bypassTS bool
}

func newMemJobRepo() *memJobRepo { return &memJobRepo{jobs: map[uuid.UUID]*price.BackfillJob{}} }

func (m *memJobRepo) Enqueue(ctx context.Context, assetID uuid.UUID, t time.Time) (*price.BackfillJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.AssetID == assetID && j.TargetTime.Equal(t) {
			return j, nil
		}
	}
	j := &price.BackfillJob{ID: uuid.New(), AssetID: assetID, TargetTime: t,
		Status: price.JobStatusPending, NextAttemptAt: time.Now().UTC()}
	m.jobs[j.ID] = j
	return j, nil
}
func (m *memJobRepo) ClaimReady(ctx context.Context) (*price.BackfillJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	for _, j := range m.jobs {
		if j.Status == price.JobStatusPending && !j.NextAttemptAt.After(now) {
			j.Status = price.JobStatusInProgress
			return j, nil
		}
	}
	return nil, nil
}
func (m *memJobRepo) MarkResolved(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[id].Status = price.JobStatusResolved
	return nil
}
func (m *memJobRepo) Reschedule(ctx context.Context, id uuid.UUID, attempts int, next time.Time, lastErr string, terminal bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	j.Attempts = attempts
	j.NextAttemptAt = next
	j.LastError = lastErr
	if terminal {
		j.Status = price.JobStatusFailed
	} else {
		j.Status = price.JobStatusPending
	}
	return nil
}
func (m *memJobRepo) UnlockWithoutCounting(ctx context.Context, id uuid.UUID, next time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.jobs[id]
	j.Status = price.JobStatusPending
	j.NextAttemptAt = next
	return nil
}
func (m *memJobRepo) ReapStale(ctx context.Context, d time.Duration) (int, error) { return 0, nil }

// get returns a snapshot of the job by ID, for asserting on attempt counts and
// terminal status after ProcessOne.
func (m *memJobRepo) get(id uuid.UUID) price.BackfillJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.jobs[id]
}

type memAssetLookup struct {
	mu sync.Mutex
	a  asset.Asset
}

func (m *memAssetLookup) GetAsset(ctx context.Context, id uuid.UUID) (*asset.Asset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.a
	return &a, nil
}

type memPriceRecorder struct {
	mu       sync.Mutex
	recorded []asset.PricePoint

	// failErr, when non-nil, is returned from RecordPrice instead of
	// appending to `recorded`. Use this to exercise the worker's
	// "recorder failed" error path.
	failErr error
}

func (m *memPriceRecorder) RecordPrice(ctx context.Context, p *asset.PricePoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	m.recorded = append(m.recorded, *p)
	return nil
}

type memResolvedHook struct{ called int }

func (m *memResolvedHook) OnResolved(ctx context.Context, assetID uuid.UUID, at time.Time, price *big.Int, src ledger.CostBasisSource) error {
	m.called++
	return nil
}

type stubProv struct {
	hp  *price.HistoricalPrice
	err error
}

func (s *stubProv) Name() price.Source { return price.SourceDefiLlama }
func (s *stubProv) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
	return nil, price.ErrNotFound
}
func (s *stubProv) GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*price.HistoricalPrice, error) {
	return s.hp, s.err
}

func TestWorker_ResolvesJob_WritesPriceHistory_FiresHook(t *testing.T) {
	ctx := context.Background()
	jr := newMemJobRepo()
	aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New(), Symbol: "XTKN"}}
	pr := &memPriceRecorder{}
	hk := &memResolvedHook{}
	resolver := price.NewResolver([]price.Provider{
		&stubProv{hp: &price.HistoricalPrice{PriceUSD: big.NewInt(42), Timestamp: time.Now(), Confidence: 1}},
	}, nil, logger.NewNoop())

	target := time.Now().UTC().Truncate(time.Minute)
	_, err := jr.Enqueue(ctx, aLookup.a.ID, target)
	require.NoError(t, err)

	w := price.NewBackfillWorker(price.WorkerDeps{
		Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
		PriceRecorder: pr, OnResolved: hk.OnResolved, Logger: logger.NewNoop(),
		RateLimitSleep: 1 * time.Millisecond,
	})
	require.NoError(t, w.ProcessOne(ctx))

	require.Len(t, pr.recorded, 1)
	require.Equal(t, "42", pr.recorded[0].PriceUSD.String())
	require.Equal(t, 1, hk.called)
}

func TestWorker_NotFound_IncrementsAttempts(t *testing.T) {
	ctx := context.Background()
	jr := newMemJobRepo()
	aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New(), Symbol: "XTKN"}}
	pr := &memPriceRecorder{}
	hk := &memResolvedHook{}
	resolver := price.NewResolver([]price.Provider{
		&stubProv{err: price.ErrNotFound},
	}, nil, logger.NewNoop())

	at := time.Now().UTC().Truncate(time.Minute)
	j, _ := jr.Enqueue(ctx, aLookup.a.ID, at)

	w := price.NewBackfillWorker(price.WorkerDeps{
		Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
		PriceRecorder: pr, OnResolved: hk.OnResolved, Logger: logger.NewNoop(),
	})
	require.NoError(t, w.ProcessOne(ctx))

	got := jr.jobs[j.ID]
	require.Equal(t, 1, got.Attempts)
	require.Equal(t, price.JobStatusPending, got.Status)
}

func TestWorker_RateLimited_DoesNotCountAttempt(t *testing.T) {
	ctx := context.Background()
	jr := newMemJobRepo()
	aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New()}}
	resolver := price.NewResolver([]price.Provider{
		&stubProv{err: price.ErrRateLimited},
	}, nil, logger.NewNoop())

	at := time.Now().UTC().Truncate(time.Minute)
	j, _ := jr.Enqueue(ctx, aLookup.a.ID, at)

	w := price.NewBackfillWorker(price.WorkerDeps{
		Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
		PriceRecorder: &memPriceRecorder{}, OnResolved: func(ctx context.Context, a uuid.UUID, t time.Time, p *big.Int, s ledger.CostBasisSource) error {
			return nil
		},
		Logger: logger.NewNoop(),
	})
	require.NoError(t, w.ProcessOne(ctx))

	got := jr.jobs[j.ID]
	require.Equal(t, 0, got.Attempts, "rate-limit must NOT count as attempt")
}

func TestWorker_TerminalAttempt_MarksFailed(t *testing.T) {
	ctx := context.Background()
	jr := newMemJobRepo()
	aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New()}}
	resolver := price.NewResolver([]price.Provider{
		&stubProv{err: price.ErrNotFound},
	}, nil, logger.NewNoop())
	at := time.Now().UTC().Truncate(time.Minute)
	j, _ := jr.Enqueue(ctx, aLookup.a.ID, at)
	j.Attempts = price.MaxAttempts - 1
	w := price.NewBackfillWorker(price.WorkerDeps{
		Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
		PriceRecorder: &memPriceRecorder{}, OnResolved: func(ctx context.Context, a uuid.UUID, t time.Time, p *big.Int, s ledger.CostBasisSource) error {
			return nil
		},
		Logger: logger.NewNoop(),
	})
	require.NoError(t, w.ProcessOne(ctx))
	require.Equal(t, price.JobStatusFailed, jr.jobs[j.ID].Status)
}

// TestWorker_ProcessOne_RecordPriceFails_UnlocksWithoutCounting verifies that
// when price_history write fails, the worker treats it as OUR fault (not the
// provider's) and unlocks the job without incrementing attempts.
func TestWorker_ProcessOne_RecordPriceFails_UnlocksWithoutCounting(t *testing.T) {
	ctx := context.Background()
	jr := newMemJobRepo()
	aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New(), Symbol: "XTKN"}}
	pr := &memPriceRecorder{failErr: errors.New("db write failure")}
	hk := &memResolvedHook{}
	resolver := price.NewResolver([]price.Provider{
		&stubProv{hp: &price.HistoricalPrice{PriceUSD: big.NewInt(42), Timestamp: time.Now(), Confidence: 1}},
	}, nil, logger.NewNoop())

	at := time.Now().UTC().Truncate(time.Minute)
	j, _ := jr.Enqueue(ctx, aLookup.a.ID, at)

	w := price.NewBackfillWorker(price.WorkerDeps{
		Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
		PriceRecorder: pr, OnResolved: hk.OnResolved, Logger: logger.NewNoop(),
		RateLimitSleep: 1 * time.Millisecond,
	})
	require.NoError(t, w.ProcessOne(ctx))

	got := jr.jobs[j.ID]
	require.Equal(t, 0, got.Attempts, "recorder failure must NOT count as attempt")
	require.Equal(t, price.JobStatusPending, got.Status, "job must be unlocked (back to pending)")
	require.Len(t, pr.recorded, 0, "nothing should have been recorded")
	require.Equal(t, 0, hk.called, "hook should not fire if price was not recorded")
}

// TestWorker_ProcessOne_HookFails_UnlocksWithoutCounting verifies that when
// the OnResolved hook returns an error, the job is unlocked without being
// counted as an attempt (same semantics as RecordPrice failure).
func TestWorker_ProcessOne_HookFails_UnlocksWithoutCounting(t *testing.T) {
	ctx := context.Background()
	jr := newMemJobRepo()
	aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New(), Symbol: "XTKN"}}
	pr := &memPriceRecorder{}

	resolver := price.NewResolver([]price.Provider{
		&stubProv{hp: &price.HistoricalPrice{PriceUSD: big.NewInt(42), Timestamp: time.Now(), Confidence: 1}},
	}, nil, logger.NewNoop())

	hookErr := errors.New("hook processing failed")
	failingHook := func(ctx context.Context, a uuid.UUID, t time.Time, p *big.Int, s ledger.CostBasisSource) error {
		return hookErr
	}

	at := time.Now().UTC().Truncate(time.Minute)
	j, _ := jr.Enqueue(ctx, aLookup.a.ID, at)

	w := price.NewBackfillWorker(price.WorkerDeps{
		Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
		PriceRecorder: pr, OnResolved: failingHook, Logger: logger.NewNoop(),
		RateLimitSleep: 1 * time.Millisecond,
	})
	require.NoError(t, w.ProcessOne(ctx))

	got := jr.jobs[j.ID]
	require.Equal(t, 0, got.Attempts, "hook failure must NOT count as attempt")
	require.Equal(t, price.JobStatusPending, got.Status, "job must be unlocked (back to pending)")
	// Price was recorded before hook ran — that's expected; only the hook failed.
	require.Len(t, pr.recorded, 1, "recorder should have been called before hook failure")
}

// TestWorker_TransientFailureDoesNotBurnAnAttempt is the worker-side half of
// the dead-provider removal. A transient provider failure must leave the job's
// attempt count untouched, so repeated outages never walk a lot to terminal
// "unpriceable". Before the removal, the always-NotFound GeckoTerminal stub
// turned this same scenario into a counted attempt.
// The control case is TestWorker_NotFound_IncrementsAttempts: a genuine
// "no data" answer still counts, as it should.
func TestWorker_TransientFailureDoesNotBurnAnAttempt(t *testing.T) {
	ctx := context.Background()
	jr := newMemJobRepo()
	aLookup := &memAssetLookup{a: asset.Asset{ID: uuid.New(), Symbol: "XTKN"}}
	pr := &memPriceRecorder{}
	hk := &memResolvedHook{}
	resolver := price.NewResolver([]price.Provider{
		&stubProv{err: price.ErrTransient},
	}, nil, logger.NewNoop())

	job, err := jr.Enqueue(ctx, aLookup.a.ID, time.Now().UTC().Truncate(time.Minute))
	require.NoError(t, err)

	w := price.NewBackfillWorker(price.WorkerDeps{
		Jobs: jr, Resolver: resolver, AssetLookup: aLookup,
		PriceRecorder: pr, OnResolved: hk.OnResolved, Logger: logger.NewNoop(),
		RateLimitSleep: 1 * time.Millisecond,
	})
	require.NoError(t, w.ProcessOne(ctx))

	got := jr.get(job.ID)
	require.Equal(t, 0, got.Attempts, "a transient failure must not count as an attempt")
	require.Equal(t, price.JobStatusPending, got.Status,
		"the job stays pending for a retry, not terminal")
	require.Empty(t, pr.recorded)
	require.Equal(t, 0, hk.called)
}
