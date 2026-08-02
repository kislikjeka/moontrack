//go:build integration

package postgres

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetPriceHistory removes all rows from price_history for a clean slate.
func resetPriceHistory(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := testDB.Pool.Exec(ctx, "DELETE FROM price_history")
	require.NoError(t, err)
}

// seedPrice inserts a price row via PriceRepository.
func seedPrice(t *testing.T, assetID uuid.UUID, ts time.Time, valueScaled int64, src price.Source) {
	t.Helper()
	ctx := context.Background()
	repo := NewPriceRepository(testDB.Pool)
	pp := &price.PricePoint{
		AssetID:  assetID,
		Time:     ts,
		PriceUSD: big.NewInt(valueScaled),
		Source:   src,
	}
	require.NoError(t, repo.RecordPricePoint(ctx, pp))
}

// TestPriceReader_CurrentPrefersCoinGeckoOverOthers verifies that when a
// lower-priority source (geckoterminal) has a more-recent price than a
// higher-priority source (coingecko), Current still returns the coingecko value.
func TestPriceReader_CurrentPrefersCoinGeckoOverOthers(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)

	now := time.Now().UTC().Truncate(time.Second)
	oneHourAgo := now.Add(-time.Hour)

	// geckoterminal: more recent (now), lower priority
	seedPrice(t, assetID, now, 100, price.SourceGeckoTerminal)
	// coingecko: older (now-1h), higher priority
	seedPrice(t, assetID, oneHourAgo, 200, price.SourceCoinGecko)

	reader := NewPriceReader(testDB.Pool, []price.Source{
		price.SourceCoinGecko,
		price.SourceGeckoTerminal,
		price.SourceDefiLlama,
	})

	val, src, err := reader.Current(ctx, assetID)
	require.NoError(t, err)

	assert.Equal(t, price.SourceCoinGecko, src, "expected coingecko (higher priority) to win")
	assert.Equal(t, big.NewInt(200), val, "expected coingecko price 200, not geckoterminal 100")
}

// TestPriceReader_HistoricalPicksByPriority verifies priority ordering for
// Historical() — the higher-priority source wins even if it is older.
func TestPriceReader_HistoricalPicksByPriority(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)

	// Both points must cover queryTime, so that priority — not the tolerance
	// window — is what decides the winner here.
	queryTime := time.Date(2025, 3, 14, 15, 30, 0, 0, time.UTC)
	sameHour := queryTime.Truncate(time.Hour)
	sameDay := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)

	// defillama: more recent (hourly point in the same hour), lower priority
	seedPrice(t, assetID, sameHour, 300, price.SourceDefiLlama)
	// coingecko: older (daily point for the same day), higher priority
	seedPrice(t, assetID, sameDay, 400, price.SourceCoinGecko)

	reader := NewPriceReader(testDB.Pool, []price.Source{
		price.SourceCoinGecko,
		price.SourceDefiLlama,
	})

	hp, src, err := reader.Historical(ctx, assetID, queryTime)
	require.NoError(t, err)

	assert.Equal(t, price.SourceCoinGecko, src, "expected coingecko (higher priority) to win")
	assert.Equal(t, big.NewInt(400), hp.PriceUSD, "expected coingecko price 400")
	assert.Equal(t, 1.0, hp.Confidence)
}

// TestPriceReader_CurrentNoRows verifies ErrNotFound when there is no data.
func TestPriceReader_CurrentNoRows(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	resetPriceHistory(t)

	assetID := seedAsset(t)
	reader := NewPriceReader(testDB.Pool, []price.Source{price.SourceCoinGecko})

	_, _, err := reader.Current(ctx, assetID)
	assert.ErrorIs(t, err, price.ErrNotFound)
}
