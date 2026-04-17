// apps/backend/internal/platform/price/e2e_test.go
//
// End-to-end price pipeline test.
//
// Wires real Resolver + real BackfillWorker with in-memory stubs to verify the
// full flow: enqueue job → ProcessOne → price recorded + hook fired (success),
// or job rescheduled without side-effects (ErrNotFound / ErrRateLimited).
//
// No external dependencies — runs with -short flag.
package price_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildPipeline wires a real Resolver + real BackfillWorker with the given provider.
// Returns (worker, jobRepo, priceRecorder, resolvedCalls).
func buildPipeline(
	t *testing.T,
	prov price.Provider,
	a asset.Asset,
) (*price.BackfillWorker, *memJobRepo, *memPriceRecorder, *int) {
	t.Helper()

	jr := newMemJobRepo()
	aLookup := &memAssetLookup{a: a}
	pr := &memPriceRecorder{}
	calls := 0

	resolver := price.NewResolver(
		[]price.Provider{prov},
		nil, // no cache
		logger.NewNoop(),
	)

	w := price.NewBackfillWorker(price.WorkerDeps{
		Jobs:        jr,
		Resolver:    resolver,
		AssetLookup: aLookup,
		PriceRecorder: pr,
		OnResolved: func(_ context.Context, _ string, _ time.Time, _ *big.Int, _ ledger.CostBasisSource) error {
			calls++
			return nil
		},
		Logger:         logger.NewNoop(),
		RateLimitSleep: 1 * time.Millisecond,
	})

	return w, jr, pr, &calls
}

// TestE2E_FullPipeline_Resolved verifies the happy path:
//   - A job is enqueued for (asset, targetTime).
//   - ProcessOne resolves it via a stub provider returning price=77.
//   - The price recorder captures exactly one PricePoint with the correct value.
//   - The OnResolved hook is called exactly once.
//   - The job transitions to status=resolved.
func TestE2E_FullPipeline_Resolved(t *testing.T) {
	ctx := context.Background()
	targetTime := time.Now().UTC().Truncate(time.Minute)
	chainID := "ethereum"
	contractAddr := "0xabc"
	a := asset.Asset{ID: uuid.New(), Symbol: "TKNA", ChainID: &chainID, ContractAddress: &contractAddr}

	prov := &stubProv{
		hp: &price.HistoricalPrice{
			PriceUSD:  big.NewInt(77),
			Timestamp: targetTime,
			Confidence: 1,
		},
	}

	w, jr, pr, calls := buildPipeline(t, prov, a)

	j, err := jr.Enqueue(ctx, a.ID, targetTime)
	require.NoError(t, err)
	require.Equal(t, price.JobStatusPending, j.Status)

	require.NoError(t, w.ProcessOne(ctx))

	// Price must have been recorded.
	require.Len(t, pr.recorded, 1, "expected exactly one price point recorded")
	assert.Equal(t, "77", pr.recorded[0].PriceUSD.String())
	assert.Equal(t, a.ID, pr.recorded[0].AssetID)

	// Hook must have been called once.
	assert.Equal(t, 1, *calls, "OnResolved must be called exactly once")

	// Job must be marked resolved.
	assert.Equal(t, price.JobStatusResolved, jr.jobs[j.ID].Status)
}

// TestE2E_FullPipeline_NotFound verifies that when all providers return ErrNotFound:
//   - The worker increments Attempts by 1.
//   - No price is recorded.
//   - OnResolved is NOT called.
//   - Job remains pending (not failed; below MaxAttempts).
func TestE2E_FullPipeline_NotFound(t *testing.T) {
	ctx := context.Background()
	targetTime := time.Now().UTC().Truncate(time.Minute)
	a := asset.Asset{ID: uuid.New(), Symbol: "TKNB"}

	w, jr, pr, calls := buildPipeline(t, &stubProv{err: price.ErrNotFound}, a)

	j, err := jr.Enqueue(ctx, a.ID, targetTime)
	require.NoError(t, err)

	require.NoError(t, w.ProcessOne(ctx))

	// Attempts incremented; job stays pending (not yet terminal).
	assert.Equal(t, 1, jr.jobs[j.ID].Attempts, "ErrNotFound must count as one attempt")
	assert.Equal(t, price.JobStatusPending, jr.jobs[j.ID].Status)

	// No side effects.
	assert.Empty(t, pr.recorded, "no price should be recorded on ErrNotFound")
	assert.Equal(t, 0, *calls, "OnResolved must NOT be called on ErrNotFound")
}

// TestE2E_FullPipeline_RateLimited verifies that ErrRateLimited:
//   - Does NOT increment Attempts.
//   - Does NOT record a price.
//   - Does NOT call OnResolved.
//   - Job returns to pending status (unlocked without counting).
func TestE2E_FullPipeline_RateLimited(t *testing.T) {
	ctx := context.Background()
	targetTime := time.Now().UTC().Truncate(time.Minute)
	a := asset.Asset{ID: uuid.New(), Symbol: "TKNC"}

	w, jr, pr, calls := buildPipeline(t, &stubProv{err: price.ErrRateLimited}, a)

	j, err := jr.Enqueue(ctx, a.ID, targetTime)
	require.NoError(t, err)

	require.NoError(t, w.ProcessOne(ctx))

	// Attempts must NOT be incremented for rate-limit.
	assert.Equal(t, 0, jr.jobs[j.ID].Attempts, "ErrRateLimited must NOT count as attempt")
	assert.Equal(t, price.JobStatusPending, jr.jobs[j.ID].Status)

	// No side effects.
	assert.Empty(t, pr.recorded, "no price should be recorded on ErrRateLimited")
	assert.Equal(t, 0, *calls, "OnResolved must NOT be called on ErrRateLimited")
}
