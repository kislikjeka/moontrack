//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// TestAssetRegistry_ResolveIsIdempotent verifies that resolving the same
// identity twice yields one row and one UUID. The registry is hit once per leg
// of every synced transaction, so a non-idempotent resolve would mint a new
// asset on every sync cycle.
func TestAssetRegistry_ResolveIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRegistryRepository(testDB.Pool)

	key := sync.NewAssetKey("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913")

	first, err := repo.Resolve(ctx, key, "USDC", "USD Coin", 6)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := repo.Resolve(ctx, key, "USDC", "USD Coin", 6)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "the same identity must keep one UUID")

	var rows int
	require.NoError(t, testDB.Pool.QueryRow(ctx,
		`SELECT count(*) FROM asset_registry WHERE chain = $1 AND contract = $2`,
		key.Chain, key.Contract).Scan(&rows))
	assert.Equal(t, 1, rows)
}

// TestAssetRegistry_SameSymbolDifferentContracts is the acceptance criterion
// proven against real SQL rather than a fake: two contracts sharing a ticker on
// one chain get two rows and two UUIDs.
//
// The store being replaced (chain_assets, UNIQUE (symbol, chain_id)) collapses
// these into one row and lets the second write overwrite the first one's
// decimals. The assertion on decimals below is what pins that fix.
func TestAssetRegistry_SameSymbolDifferentContracts(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRegistryRepository(testDB.Pool)

	real, err := repo.Resolve(ctx,
		sync.NewAssetKey("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"), "USDC", "USD Coin", 6)
	require.NoError(t, err)

	impostor, err := repo.Resolve(ctx,
		sync.NewAssetKey("base", "0xdead89fcd6edb6e08f4c7c32d4f71b54bda02913"), "USDC", "Not USD Coin", 18)
	require.NoError(t, err)

	assert.NotEqual(t, real.ID, impostor.ID)
	assert.Equal(t, 6, real.Decimals)
	assert.Equal(t, 18, impostor.Decimals, "each contract keeps its own decimals")
}

// TestAssetRegistry_SameCoinAcrossChains covers cross-chain splitting for both
// a token and the native coin. The native half is the sharper case: every
// chain's native leg carries the identical sentinel, so uniqueness rests
// entirely on the chain.
func TestAssetRegistry_SameCoinAcrossChains(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRegistryRepository(testDB.Pool)

	baseUSDC, err := repo.Resolve(ctx,
		sync.NewAssetKey("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"), "USDC", "USD Coin", 6)
	require.NoError(t, err)
	arbUSDC, err := repo.Resolve(ctx,
		sync.NewAssetKey("arbitrum", "0xaf88d065e77c8cc2239327c5edb3a432268e5831"), "USDC", "USD Coin", 6)
	require.NoError(t, err)
	assert.NotEqual(t, baseUSDC.ID, arbUSDC.ID)

	ethNative, err := repo.Resolve(ctx, sync.NewAssetKey("ethereum", sync.NativeContract), "ETH", "Ethereum", 18)
	require.NoError(t, err)
	baseNative, err := repo.Resolve(ctx, sync.NewAssetKey("base", sync.NativeContract), "ETH", "Ethereum", 18)
	require.NoError(t, err)
	arbNative, err := repo.Resolve(ctx, sync.NewAssetKey("arbitrum", sync.NativeContract), "ETH", "Ethereum", 18)
	require.NoError(t, err)

	assert.NotEqual(t, ethNative.ID, baseNative.ID)
	assert.NotEqual(t, baseNative.ID, arbNative.ID)
	assert.NotEqual(t, ethNative.ID, arbNative.ID)
}

// TestAssetRegistry_NativeIsCoveredByUniqueness is the regression guard on the
// specific weakness of the index it replaces. idx_assets_onchain_identity is
// PARTIAL — `WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL` — so
// native rows sit outside uniqueness entirely and may duplicate freely. Here
// the constraint is total, and the sentinel being a literal rather than NULL is
// what lets it apply.
func TestAssetRegistry_NativeIsCoveredByUniqueness(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRegistryRepository(testDB.Pool)

	key := sync.NewAssetKey("base", sync.NativeContract)
	first, err := repo.Resolve(ctx, key, "ETH", "Ethereum", 18)
	require.NoError(t, err)
	second, err := repo.Resolve(ctx, key, "ETH", "Ethereum", 18)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	// A raw insert bypassing the repository must still be refused.
	_, err = testDB.Pool.Exec(ctx,
		`INSERT INTO asset_registry (chain, contract, symbol, decimals) VALUES ($1, $2, 'ETH', 18)`,
		key.Chain, key.Contract)
	require.Error(t, err, "a duplicate native identity must violate the unique constraint")
}

// TestAssetRegistry_ResolveDoesNotOverwriteMetadata pins the deliberate choice
// that a conflict updates nothing. Re-reporting a known contract with different
// decimals must not retroactively invalidate the base-unit conversions already
// performed against the stored value — the live defect in chain_assets, whose
// upsert assigns decimals = EXCLUDED.decimals unconditionally.
func TestAssetRegistry_ResolveDoesNotOverwriteMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRegistryRepository(testDB.Pool)

	key := sync.NewAssetKey("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913")
	first, err := repo.Resolve(ctx, key, "USDC", "USD Coin", 6)
	require.NoError(t, err)

	// A later sighting reporting different metadata for the same identity.
	second, err := repo.Resolve(ctx, key, "WRONG", "Wrong Name", 18)
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "USDC", second.Symbol, "metadata is written on create only")
	assert.Equal(t, 6, second.Decimals, "decimals must survive a conflicting later report")
}

// TestAssetRegistry_ResolveRejectsInvalidKey verifies a blank identity fails at
// the call rather than reaching the table, where the non-blank CHECK would
// reject it as an opaque constraint violation.
func TestAssetRegistry_ResolveRejectsInvalidKey(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRegistryRepository(testDB.Pool)

	_, err := repo.Resolve(ctx, sync.AssetKey{Chain: "", Contract: sync.NativeContract}, "ETH", "Ethereum", 18)
	require.ErrorIs(t, err, ErrInvalidAssetKey)

	_, err = repo.Resolve(ctx, sync.AssetKey{Chain: "base", Contract: ""}, "ETH", "Ethereum", 18)
	require.ErrorIs(t, err, ErrInvalidAssetKey)
}

// TestAssetRegistry_ResolveNormalizesContractCase verifies the same contract in
// different casings resolves to ONE asset. Without it a checksummed and a
// lowercase spelling of one token would occupy two rows, each with its own UUID
// and its own tax lots.
func TestAssetRegistry_ResolveNormalizesContractCase(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	repo := NewAssetRegistryRepository(testDB.Pool)

	lower, err := repo.Resolve(ctx,
		sync.NewAssetKey("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"), "USDC", "USD Coin", 6)
	require.NoError(t, err)
	checksummed, err := repo.Resolve(ctx,
		sync.NewAssetKey("base", "0x833589FCD6EDB6E08F4C7C32D4F71B54BDA02913"), "USDC", "USD Coin", 6)
	require.NoError(t, err)

	assert.Equal(t, lower.ID, checksummed.ID, "casing must not split one contract into two assets")
}
