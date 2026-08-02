//go:build integration

package postgres

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetPriceAt_SpotWriteDoesNotAnswerLaterTransaction reproduces the
// production defect: the background PriceUpdater writes a spot price at
// time.Now(), and a lot whose transaction moment is *later* than that write
// used to receive it as a historical price with no signal at all.
//
// The tolerance window turns that into a miss, so the caller goes to a
// provider instead of silently booking a stale spot price into cost basis.
func TestGetPriceAt_SpotWriteDoesNotAnswerLaterTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)
	repo := NewPriceRepository(testDB.Pool)

	// A spot write: arbitrary sub-hour instant, as PriceUpdater produces.
	spotWrite := time.Date(2025, 3, 13, 9, 17, 3, 0, time.UTC)
	seedPrice(t, assetID, spotWrite, 500, price.SourceCoinGecko)

	// The lot's transaction moment is later than the spot write.
	txMoment := spotWrite.Add(31 * time.Hour)

	_, err := repo.GetPriceAt(ctx, assetID, txMoment)
	require.ErrorIs(t, err, asset.ErrNoPriceData,
		"a spot point must not stand in for a later transaction moment")
}

// TestGetPriceAt_DailyPointCoversItsDay guards the other direction: the window
// must be wide enough for the day-granular CoinGecko /history fallback, or the
// backfill worker would treat its own cached point as a miss and re-query the
// provider on every single lookup.
func TestGetPriceAt_DailyPointCoversItsDay(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)
	repo := NewPriceRepository(testDB.Pool)

	midnight := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)
	seedPrice(t, assetID, midnight, 700, price.SourceCoinGecko)

	// Late in the same day — inside the daily point's window.
	got, err := repo.GetPriceAt(ctx, assetID, midnight.Add(23*time.Hour+59*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(700), got.PriceUSD)

	// The next day is a different daily point — outside the window.
	_, err = repo.GetPriceAt(ctx, assetID, midnight.Add(25*time.Hour))
	require.ErrorIs(t, err, asset.ErrNoPriceData,
		"a daily point must not answer for the following day")
}

// TestGetPriceAt_HourlyPointCoversOnlyItsHour pins the narrower window that an
// hour-aligned point (from a range query) implies. The width comes from the
// stored point's own granularity, not from one shared constant.
func TestGetPriceAt_HourlyPointCoversOnlyItsHour(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)
	repo := NewPriceRepository(testDB.Pool)

	hour := time.Date(2025, 3, 14, 15, 0, 0, 0, time.UTC)
	seedPrice(t, assetID, hour, 900, price.SourceCoinGecko)

	got, err := repo.GetPriceAt(ctx, assetID, hour.Add(45*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(900), got.PriceUSD)

	_, err = repo.GetPriceAt(ctx, assetID, hour.Add(2*time.Hour))
	require.ErrorIs(t, err, asset.ErrNoPriceData,
		"an hourly point must not answer beyond its own hour")
}

// TestPriceReader_HistoricalRejectsSpotOutsideWindow is the same guard on the
// priority-ordered reader.
func TestPriceReader_HistoricalRejectsSpotOutsideWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)

	spotWrite := time.Date(2025, 3, 13, 9, 17, 3, 0, time.UTC)
	seedPrice(t, assetID, spotWrite, 500, price.SourceCoinGecko)

	reader := NewPriceReader(testDB.Pool, []price.Source{
		price.SourceCoinGecko,
		price.SourceDefiLlama,
	})

	_, _, err := reader.Historical(ctx, assetID, spotWrite.Add(5*time.Hour))
	require.ErrorIs(t, err, price.ErrNotFound)
}

// TestPriceReader_HistoricalSkipsUncoveringHigherPriority verifies that a
// higher-priority source whose point does not cover the target does not
// shadow a lower-priority source whose point does.
func TestPriceReader_HistoricalSkipsUncoveringHigherPriority(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)

	target := time.Date(2025, 3, 14, 15, 30, 0, 0, time.UTC)
	// coingecko (higher priority) has only an hourly point two hours back —
	// outside its own hour, so it does not cover the target.
	seedPrice(t, assetID, target.Truncate(time.Hour).Add(-2*time.Hour), 100, price.SourceCoinGecko)
	// defillama (lower priority) has the hour that actually contains target.
	seedPrice(t, assetID, target.Truncate(time.Hour), 200, price.SourceDefiLlama)

	reader := NewPriceReader(testDB.Pool, []price.Source{
		price.SourceCoinGecko,
		price.SourceDefiLlama,
	})

	hp, src, err := reader.Historical(ctx, assetID, target)
	require.NoError(t, err)
	assert.Equal(t, price.SourceDefiLlama, src)
	assert.Equal(t, big.NewInt(200), hp.PriceUSD)
}

// TestPriceReader_HistoricalSeesPastANearerSpotPoint guards the candidate
// selection: a spot write later in the day is nearer to the target than the
// day's aligned point, but only the aligned point covers the target. Taking
// just the nearest row per source would lose the usable answer.
func TestPriceReader_HistoricalSeesPastANearerSpotPoint(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)

	midnight := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)
	// The covering daily point.
	seedPrice(t, assetID, midnight, 700, price.SourceCoinGecko)
	// A nearer spot write from the same source, which covers nothing.
	seedPrice(t, assetID, midnight.Add(9*time.Hour+13*time.Minute+7*time.Second), 999, price.SourceCoinGecko)

	target := midnight.Add(20 * time.Hour)

	reader := NewPriceReader(testDB.Pool, []price.Source{
		price.SourceCoinGecko,
		price.SourceDefiLlama,
	})

	hp, src, err := reader.Historical(ctx, assetID, target)
	require.NoError(t, err)
	assert.Equal(t, price.SourceCoinGecko, src)
	assert.Equal(t, big.NewInt(700), hp.PriceUSD,
		"the covering daily point must win over a nearer, non-covering spot write")
}

// TestGetPriceAt_SeesPastANearerSpotPoint is the GetPriceAt counterpart: the
// background updater's spot write is nearer to the target than the day's
// aligned point, but covers nothing. The cached daily point must still be
// found, otherwise every lookup would re-query the provider.
func TestGetPriceAt_SeesPastANearerSpotPoint(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)
	repo := NewPriceRepository(testDB.Pool)

	midnight := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)
	seedPrice(t, assetID, midnight, 700, price.SourceCoinGecko)
	seedPrice(t, assetID, midnight.Add(9*time.Hour+13*time.Minute+7*time.Second), 999, price.SourceCoinGecko)

	got, err := repo.GetPriceAt(ctx, assetID, midnight.Add(20*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(700), got.PriceUSD,
		"the covering daily point must win over a nearer, non-covering spot write")
}
