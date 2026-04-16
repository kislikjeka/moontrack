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

// Ensure asset.Asset type is accessible (compile-time check)
var _ = asset.Asset{}
