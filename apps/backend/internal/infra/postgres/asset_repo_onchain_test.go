//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/stretchr/testify/require"
)

func TestAssetRepo_UpsertByOnChainIdentity_DedupesByChainAndAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRepository(testDB.Pool)

	chain := "ethereum"
	addr := "0xabcdef0000000000000000000000000000000001"

	a1, created1, err := repo.UpsertByOnChainIdentity(ctx, chain, addr, "TKN", "Token", 18)
	require.NoError(t, err)
	require.True(t, created1)

	a2, created2, err := repo.UpsertByOnChainIdentity(ctx, chain, addr, "TKN", "Token", 18)
	require.NoError(t, err)
	require.False(t, created2)
	require.Equal(t, a1.ID, a2.ID)
}

func TestAssetRepo_UpsertByOnChainIdentity_AllowsNullCoinGeckoID(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRepository(testDB.Pool)

	a, _, err := repo.UpsertByOnChainIdentity(ctx, "polygon", "0xbeef000000000000000000000000000000000002", "X", "X Token", 18)
	require.NoError(t, err)
	require.Equal(t, "", a.CoinGeckoID)
	require.NotNil(t, a.ChainID)
	require.Equal(t, "polygon", *a.ChainID)
}

// TestAssetRepo_GetActiveAssets_IncludesNullCoinGeckoID verifies that GetActiveAssets
// correctly handles rows where coingecko_id is NULL (e.g. on-chain-only assets).
func TestAssetRepo_GetActiveAssets_IncludesNullCoinGeckoID(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRepository(testDB.Pool)

	// Insert asset with a coingecko_id set.
	a1, _, err := repo.UpsertByOnChainIdentity(ctx, "ethereum", "0xaaaa000000000000000000000000000000000001", "BTC2", "Bitcoin2", 8)
	require.NoError(t, err)
	// Manually set coingecko_id for this asset so we have one non-NULL row.
	_, err = testDB.Pool.Exec(ctx, `UPDATE assets SET coingecko_id = 'bitcoin' WHERE id = $1`, a1.ID)
	require.NoError(t, err)

	// Insert asset via UpsertByOnChainIdentity — coingecko_id will be NULL.
	_, _, err = repo.UpsertByOnChainIdentity(ctx, "polygon", "0xbbbb000000000000000000000000000000000002", "ONCHAIN", "OnChain Token", 18)
	require.NoError(t, err)

	// GetActiveAssets must return both rows without error.
	assets, err := repo.GetActiveAssets(ctx)
	require.NoError(t, err)
	require.Len(t, assets, 2)

	// Verify the one with coingecko_id='bitcoin' has it set; the other has empty string.
	cgIDs := make(map[string]string) // symbol -> cgID
	for _, a := range assets {
		cgIDs[a.Symbol] = a.CoinGeckoID
	}
	require.Equal(t, "bitcoin", cgIDs["BTC2"])
	require.Equal(t, "", cgIDs["ONCHAIN"])
}

// Ensure asset.Asset type is accessible (compile-time check)
var _ = asset.Asset{}
