package sync

import (
	"context"
	"errors"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// KnownnessQueue is the write side of the knownness registry: the queue the
// background probe worker drains. Split from KnownnessRegistry (the read side)
// because the sync hot path must be able to depend on the read side alone and
// can never be handed the ability to write a verdict.
type KnownnessQueue interface {
	// ClaimReady locks and returns the next due pending identity, nil when none.
	ClaimReady(ctx context.Context) (*KnownnessRecord, error)

	// MarkKnown records a terminal "known" verdict from the given level.
	MarkKnown(ctx context.Context, key AssetKey, source KnownnessSource) error

	// MarkUnknown records the terminal "unknown" verdict.
	MarkUnknown(ctx context.Context, key AssetKey, attempts int, lastError string) error

	// Reschedule records a COUNTED failed attempt and backs the next probe off.
	Reschedule(ctx context.Context, key AssetKey, attempts int, nextAttemptAt time.Time, lastError string) error

	// UnlockWithoutCounting releases the row WITHOUT advancing attempts.
	UnlockWithoutCounting(ctx context.Context, key AssetKey, nextAttemptAt time.Time) error
}

// QuotabilityProbe asks the price provider whether an identity is quotable by
// (chain, contract). It is the ONLY network dependency of the whole filter, and
// it lives behind the background worker — never on the sync path.
//
// The question is deliberately "is there a price", not "is this coin
// legitimate". Decision #37 measured the difference on real data: a legitimacy
// verifier rejects a debt token and an LP share, both of which have to be
// valued. A debt token quoted at −0.9997 is a perfectly good answer here.
type QuotabilityProbe interface {
	// IsQuotable returns nil when the identity is quotable. Error semantics
	// follow the price package sentinels: ErrRateLimited and ErrTransient mean
	// "ask again, this says nothing about the asset"; anything else (ErrNotFound,
	// ErrLowConfidence, ErrUnsupportedChain) is a real negative answer.
	IsQuotable(ctx context.Context, key AssetKey) error
}

// minRateLimitFloor is the lowest delay honoured for a provider Retry-After
// hint, mirroring the price backfill worker's floor. Stops a provider answering
// "Retry-After: 0" from spinning the worker.
const minRateLimitFloor = 5 * time.Second

// KnownnessWorkerDeps holds the worker's dependencies.
type KnownnessWorkerDeps struct {
	Queue          KnownnessQueue
	Probe          QuotabilityProbe
	Logger         *logger.Logger
	RateLimitSleep time.Duration
}

// KnownnessWorker drains the knownness queue, probing one identity at a time.
//
// THE VERDICT "UNKNOWN" IS SET ONLY BY EXHAUSTING RETRIES — never by a single
// API failure. The retry policy is not reimplemented here: it is the price
// package's ladder (price.BackoffDelay, price.IsTerminalAttempt), reused so the
// two cannot drift apart. 15 min → 1 h → 6 h → 24 h, terminal at
// price.MaxAttempts, which is ~7 days of continuous genuine negatives.
//
// The distinction that matters downstream:
//
//   - rate limit / transient  → UnlockWithoutCounting. Attempts do NOT advance,
//     so the identity is no closer to conviction than before. Stays `pending`,
//     which reads as "could not check".
//   - not found / low confidence / unsupported chain → a counted attempt, and
//     eventually `unknown`, which reads as "checked, and the answer is no".
//
// A provider outage therefore delays verdicts and convicts nothing.
type KnownnessWorker struct {
	d KnownnessWorkerDeps
}

// NewKnownnessWorker builds the worker. RateLimitSleep defaults to 5s.
func NewKnownnessWorker(d KnownnessWorkerDeps) *KnownnessWorker {
	if d.RateLimitSleep == 0 {
		d.RateLimitSleep = 5 * time.Second
	}
	d.Logger = d.Logger.WithField("component", "knownness")
	return &KnownnessWorker{d: d}
}

// ProcessOne claims one queued identity (if any) and probes it. Safe in a loop.
func (w *KnownnessWorker) ProcessOne(ctx context.Context) error {
	rec, err := w.d.Queue.ClaimReady(ctx)
	if err != nil || rec == nil {
		return err
	}

	perr := w.d.Probe.IsQuotable(ctx, rec.Key)

	switch {
	case perr == nil:
		// Quotable: level 2 answers yes.
		w.d.Logger.Info("asset resolved known by quotability",
			"chain", price.SanitizeLogField(rec.Key.Chain),
			"contract", price.SanitizeLogField(rec.Key.Contract),
			"asset_symbol", price.SanitizeLogField(rec.Symbol),
		)
		return w.d.Queue.MarkKnown(ctx, rec.Key, KnownnessSourceQuotable)

	case errors.Is(perr, price.ErrRateLimited):
		// Not an attempt: the provider declined to answer, which says nothing
		// about the asset. Honour a Retry-After hint, clamped to the floor.
		delay := w.d.RateLimitSleep
		var rle *price.RateLimitedError
		if errors.As(perr, &rle) && rle.RetryAfter > 0 {
			delay = rle.RetryAfter
		}
		if delay < minRateLimitFloor {
			delay = minRateLimitFloor
		}
		return w.d.Queue.UnlockWithoutCounting(ctx, rec.Key, time.Now().Add(delay))

	case errors.Is(perr, price.ErrTransient):
		// Not an attempt either — the network failed, not the asset.
		return w.d.Queue.UnlockWithoutCounting(ctx, rec.Key, time.Now().Add(5*time.Minute))

	default:
		// A real negative: NotFound / LowConfidence / UnsupportedChain. This is
		// the only class that spends an attempt.
		newAttempts := rec.Attempts + 1
		if price.IsTerminalAttempt(newAttempts) {
			w.d.Logger.Info("asset resolved unknown after exhausting retries",
				"chain", price.SanitizeLogField(rec.Key.Chain),
				"contract", price.SanitizeLogField(rec.Key.Contract),
				"asset_symbol", price.SanitizeLogField(rec.Symbol),
				"attempts", newAttempts,
			)
			return w.d.Queue.MarkUnknown(ctx, rec.Key, newAttempts, perr.Error())
		}
		return w.d.Queue.Reschedule(ctx, rec.Key, newAttempts,
			time.Now().Add(price.BackoffDelay(newAttempts)), perr.Error())
	}
}

// Run loops ProcessOne at the given rate until ctx is done.
func (w *KnownnessWorker) Run(ctx context.Context, rate time.Duration) {
	ticker := time.NewTicker(rate)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.ProcessOne(ctx); err != nil {
				w.d.Logger.Warn("knownness worker iteration error",
					"error", price.SanitizeLogField(err.Error()))
			}
		}
	}
}
