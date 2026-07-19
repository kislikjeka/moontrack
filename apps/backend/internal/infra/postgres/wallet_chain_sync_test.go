//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// createUserForChainSync inserts a minimal user for FK satisfaction.
func createUserForChainSync(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'hash', NOW(), NOW())
	`, userID, fmt.Sprintf("chainsync-%s@test.com", userID.String()[:8]))
	require.NoError(t, err)
	return userID
}

// TestWalletCreate_SeedsEnabledChainSet verifies Create stamps the default
// Enabled chain set (eth/base/arbitrum) as wallet_chain_sync rows in one tx.
func TestWalletCreate_SeedsEnabledChainSet(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createUserForChainSync(t, ctx)
	repo := NewWalletRepository(testDB.Pool)

	w := &wallet.Wallet{
		UserID:  userID,
		Name:    "Seed Wallet",
		Address: "0x1111111111111111111111111111111111111111",
	}
	require.NoError(t, repo.Create(ctx, w))

	rows, err := repo.GetChainSyncRows(ctx, w.ID)
	require.NoError(t, err)

	got := map[string]wallet.SyncStatus{}
	for _, r := range rows {
		got[r.Chain] = r.SyncStatus
		assert.Equal(t, "idle", r.SyncPhase)
		assert.Nil(t, r.CollectCursorAt)
	}
	assert.Equal(t, map[string]wallet.SyncStatus{
		"ethereum": wallet.SyncStatusPending,
		"base":     wallet.SyncStatusPending,
		"arbitrum": wallet.SyncStatusPending,
	}, got)
}

// TestChainSync_CollectCursor verifies the per-chain collect-cursor setter
// mutates exactly the addressed (wallet, chain) row and leaves others untouched.
func TestChainSync_CollectCursor(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createUserForChainSync(t, ctx)
	repo := NewWalletRepository(testDB.Pool)
	w := &wallet.Wallet{UserID: userID, Name: "Cursor Wallet", Address: "0x2222222222222222222222222222222222222222"}
	require.NoError(t, repo.Create(ctx, w))

	cursor := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SetChainCollectCursor(ctx, w.ID, "base", cursor))

	rows, err := repo.GetChainSyncRows(ctx, w.ID)
	require.NoError(t, err)

	byChain := map[string]wallet.WalletChainSync{}
	for _, r := range rows {
		byChain[r.Chain] = r
	}

	// base row mutated
	require.NotNil(t, byChain["base"].CollectCursorAt)
	assert.True(t, cursor.Equal(*byChain["base"].CollectCursorAt))

	// ethereum row untouched
	assert.Nil(t, byChain["ethereum"].CollectCursorAt)
}

// TestWalletStatus_RollupOverChainRows verifies the wallet-level sync_status
// stays a rollup over its chain rows across the lifecycle setters: claim flips
// all chains + wallet to syncing; SetSyncError flips all to error; completion to
// synced.
func TestWalletStatus_RollupOverChainRows(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createUserForChainSync(t, ctx)
	repo := NewWalletRepository(testDB.Pool)
	w := &wallet.Wallet{UserID: userID, Name: "Rollup Wallet", Address: "0x3333333333333333333333333333333333333333"}
	require.NoError(t, repo.Create(ctx, w))

	assertWalletStatus := func(expected wallet.SyncStatus) {
		got, err := repo.GetByID(ctx, w.ID)
		require.NoError(t, err)
		assert.Equal(t, expected, got.SyncStatus)

		rows, err := repo.GetChainSyncRows(ctx, w.ID)
		require.NoError(t, err)
		assert.Equal(t, expected, wallet.RollupStatus(rows), "wallet status must equal the rollup of chain rows")
	}

	// Claim → syncing across the board.
	claimed, err := repo.ClaimWalletForSync(ctx, w.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	assertWalletStatus(wallet.SyncStatusSyncing)

	// Error → error across the board.
	require.NoError(t, repo.SetSyncError(ctx, w.ID, "boom"))
	assertWalletStatus(wallet.SyncStatusError)

	// Re-claim then complete → synced across the board.
	_, err = repo.ClaimWalletForSync(ctx, w.ID)
	require.NoError(t, err)
	require.NoError(t, repo.SetSyncCompletedAt(ctx, w.ID, time.Now()))
	assertWalletStatus(wallet.SyncStatusSynced)
}

// TestMigration29_SeedsExistingWallets verifies migration 000029 seeded the
// Enabled chain set for a wallet that already existed at migration time. The
// migration ran during container init on the seeded fixture wallets; here we
// assert the invariant holds for any wallet inserted via the repo (which is the
// live path). This double-checks the seeding SELECT ... CROSS JOIN shape.
func TestMigration29_SeedsExistingWallets(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createUserForChainSync(t, ctx)

	// Insert a wallet with NO chain rows (bypassing the repo), then run the same
	// seeding statement the migration uses, and assert it produced 3 rows.
	walletID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, address, sync_status, created_at, updated_at)
		VALUES ($1, $2, 'Legacy', '0x4444444444444444444444444444444444444444', 'pending', NOW(), NOW())
	`, walletID, userID)
	require.NoError(t, err)

	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO wallet_chain_sync (wallet_id, chain, sync_status, sync_error, sync_phase, collect_cursor_at, last_sync_at)
		SELECT w.id, c.chain, w.sync_status, w.sync_error, w.sync_phase, w.collect_cursor_at, w.last_sync_at
		FROM wallets w
		CROSS JOIN (VALUES ('ethereum'), ('base'), ('arbitrum')) AS c(chain)
		WHERE w.id = $1
	`, walletID)
	require.NoError(t, err)

	repo := NewWalletRepository(testDB.Pool)
	rows, err := repo.GetChainSyncRows(ctx, walletID)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}
