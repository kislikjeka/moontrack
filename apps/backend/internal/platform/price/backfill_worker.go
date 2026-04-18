// apps/backend/internal/platform/price/backfill_worker.go
package price

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// AssetLookup resolves an asset by ID (subset of asset.Service).
type AssetLookup interface {
	GetAsset(ctx context.Context, id uuid.UUID) (*asset.Asset, error)
}

// PriceRecorder writes a PricePoint to price_history.
type PriceRecorder interface {
	RecordPrice(ctx context.Context, p *asset.PricePoint) error
}

// OnPriceResolvedFunc notifies interested parties (ledger hook) that a price
// is now known for (assetID, at). Implementations must be idempotent.
// The UUID (not symbol) is used to avoid cross-chain collisions.
type OnPriceResolvedFunc func(ctx context.Context, assetID uuid.UUID, at time.Time, priceUSDPerUnit *big.Int, src ledger.CostBasisSource) error

// WorkerDeps holds all dependencies for BackfillWorker.
type WorkerDeps struct {
	Jobs           JobRepository
	Resolver       *Resolver
	AssetLookup    AssetLookup
	PriceRecorder  PriceRecorder
	OnResolved     OnPriceResolvedFunc
	Logger         *logger.Logger
	RateLimitSleep time.Duration
}

// minRateLimitFloor is the lowest delay we will honor when a provider returns
// a Retry-After header. Prevents hot-looping against a misconfigured upstream.
const minRateLimitFloor = 5 * time.Second

// BackfillWorker processes pending price backfill jobs one at a time.
type BackfillWorker struct {
	d WorkerDeps
}

// NewBackfillWorker creates a new BackfillWorker. RateLimitSleep defaults to 5s if not set.
func NewBackfillWorker(d WorkerDeps) *BackfillWorker {
	if d.RateLimitSleep == 0 {
		d.RateLimitSleep = 5 * time.Second
	}
	d.Logger = d.Logger.WithField("component", "price_backfill")
	return &BackfillWorker{d: d}
}

// ProcessOne claims one job (if any) and processes it. Safe to run in a loop.
//
// Error semantics:
//   - rerr == nil → record price, fire hook, MarkResolved.
//     If recording or hook fails, UnlockWithoutCounting (our error, not provider's).
//   - ErrRateLimited → UnlockWithoutCounting (+ sleep delay). Does NOT count as attempt.
//   - ErrTransient → UnlockWithoutCounting (5 min). Does NOT count as attempt.
//   - default (NotFound/LowConfidence/UnsupportedChain) → attempts+1, BackoffDelay(new).
//     MaxAttempts reached → Reschedule as terminal (status=failed).
func (w *BackfillWorker) ProcessOne(ctx context.Context) error {
	job, err := w.d.Jobs.ClaimReady(ctx)
	if err != nil || job == nil {
		return err
	}

	a, err := w.d.AssetLookup.GetAsset(ctx, job.AssetID)
	if err != nil {
		// Asset is gone; mark failed so we stop retrying.
		return w.d.Jobs.Reschedule(ctx, job.ID, job.Attempts, time.Now().Add(24*time.Hour),
			"asset lookup failed: "+err.Error(), true)
	}

	hp, src, rerr := w.d.Resolver.ResolveHistorical(ctx, *a, job.TargetTime)

	switch {
	case rerr == nil:
		// Record price_history, notify hook, mark resolved.
		pp := &asset.PricePoint{
			Time:     hp.Timestamp,
			AssetID:  a.ID,
			PriceUSD: hp.PriceUSD,
			Source:   asset.PriceSource(src),
		}
		if err := w.d.PriceRecorder.RecordPrice(ctx, pp); err != nil {
			// Don't count as attempt — it's our problem, not the provider's.
			return w.d.Jobs.UnlockWithoutCounting(ctx, job.ID, time.Now().Add(1*time.Minute))
		}
		if err := w.d.OnResolved(ctx, a.ID, job.TargetTime, hp.PriceUSD, ledger.CostBasisFMVAtTransfer); err != nil {
			return w.d.Jobs.UnlockWithoutCounting(ctx, job.ID, time.Now().Add(1*time.Minute))
		}
		return w.d.Jobs.MarkResolved(ctx, job.ID)

	case errors.Is(rerr, ErrRateLimited):
		// Don't count; retry after rate-limit sleep. Honor provider-supplied
		// Retry-After hint when present. Clamp to a minimum floor so a
		// misconfigured provider returning "Retry-After: 0" can't spin the worker.
		delay := w.d.RateLimitSleep
		var rle *RateLimitedError
		if errors.As(rerr, &rle) && rle.RetryAfter > 0 {
			delay = rle.RetryAfter
		}
		if delay < minRateLimitFloor {
			delay = minRateLimitFloor
		}
		return w.d.Jobs.UnlockWithoutCounting(ctx, job.ID, time.Now().Add(delay))

	case errors.Is(rerr, ErrTransient):
		// Don't count; short backoff for transient errors.
		return w.d.Jobs.UnlockWithoutCounting(ctx, job.ID, time.Now().Add(5*time.Minute))

	default:
		// NotFound / LowConfidence / UnsupportedChain — counts as attempt.
		newAttempts := job.Attempts + 1
		if IsTerminalAttempt(newAttempts) {
			return w.d.Jobs.Reschedule(ctx, job.ID, newAttempts,
				time.Now().Add(24*time.Hour), rerr.Error(), true)
		}
		return w.d.Jobs.Reschedule(ctx, job.ID, newAttempts,
			time.Now().Add(BackoffDelay(newAttempts)), rerr.Error(), false)
	}
}

// Run loops ProcessOne at the given rate until ctx is done.
func (w *BackfillWorker) Run(ctx context.Context, rate time.Duration) {
	ticker := time.NewTicker(rate)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.ProcessOne(ctx); err != nil {
				// Sanitize error: may wrap provider-supplied strings with control bytes.
				w.d.Logger.Warn("worker iteration error", "error", sanitizeLogField(err.Error()))
			}
		}
	}
}
