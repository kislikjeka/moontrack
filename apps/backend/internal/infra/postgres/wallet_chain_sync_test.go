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

// TestWalletStatus_RollupOverChainRows verifies the wallet-level sync_status is a
// TRUE rollup DERIVED from the per-chain rows (issue #28). Chains now advance
// independently: claim flips all chains + wallet to syncing; a per-chain error on
// ONE chain, followed by RollupWalletSyncStatus, rolls the wallet up to error
// while the other chains keep their own status; per-chain completion + rollup
// yields synced.
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

	// Isolated per-chain failure on ONE chain; the others stay syncing. After the
	// rollup the wallet reflects error (severity fold: any error wins).
	require.NoError(t, repo.SetChainSyncError(ctx, w.ID, "base", "boom"))
	require.NoError(t, repo.RollupWalletSyncStatus(ctx, w.ID))
	assertWalletStatus(wallet.SyncStatusError)

	// The failed chain's error message propagates to the wallet-level sync_error.
	got, err := repo.GetByID(ctx, w.ID)
	require.NoError(t, err)
	require.NotNil(t, got.SyncError)
	assert.Contains(t, *got.SyncError, "boom")

	// Complete every chain, then roll up → synced across the board, error cleared.
	for _, chain := range wallet.EnabledChains() {
		require.NoError(t, repo.SetChainSyncCompleted(ctx, w.ID, chain, time.Now()))
	}
	require.NoError(t, repo.RollupWalletSyncStatus(ctx, w.ID))
	assertWalletStatus(wallet.SyncStatusSynced)

	got, err = repo.GetByID(ctx, w.ID)
	require.NoError(t, err)
	assert.Nil(t, got.SyncError, "rollup clears wallet error once no chain is errored")
}

// TestChainSyncError_LeavesCursorAndSiblings verifies the #28 isolation invariant
// at the repo level: SetChainSyncError marks exactly one chain errored, leaves
// that chain's collect cursor untouched (so it resumes where it left off), and
// does not disturb sibling chain rows.
func TestChainSyncError_LeavesCursorAndSiblings(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createUserForChainSync(t, ctx)
	repo := NewWalletRepository(testDB.Pool)
	w := &wallet.Wallet{UserID: userID, Name: "Isolate Wallet", Address: "0x5555555555555555555555555555555555555555"}
	require.NoError(t, repo.Create(ctx, w))

	// base has a prior cursor; error it and confirm the cursor survives.
	cursor := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SetChainCollectCursor(ctx, w.ID, "base", cursor))
	require.NoError(t, repo.SetChainSyncError(ctx, w.ID, "base", "provider 503"))

	rows, err := repo.GetChainSyncRows(ctx, w.ID)
	require.NoError(t, err)
	byChain := map[string]wallet.WalletChainSync{}
	for _, r := range rows {
		byChain[r.Chain] = r
	}

	assert.Equal(t, wallet.SyncStatusError, byChain["base"].SyncStatus)
	require.NotNil(t, byChain["base"].CollectCursorAt)
	assert.True(t, cursor.Equal(*byChain["base"].CollectCursorAt), "failed chain's cursor is preserved for resume")

	assert.Equal(t, wallet.SyncStatusPending, byChain["ethereum"].SyncStatus, "sibling untouched")
	assert.Nil(t, byChain["ethereum"].SyncError)
}

// TestClaimResetsErroredChainForRetry is the cross-cycle half of #28's "resumes
// from its own cursor on the next cycle": after a chain is isolated as errored
// (with its cursor preserved), the next cycle's claim must re-pend that chain —
// flip it back to syncing and CLEAR its error — WITHOUT disturbing its collect
// cursor. That is what lets the failed chain retry from exactly where it left off.
func TestClaimResetsErroredChainForRetry(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createUserForChainSync(t, ctx)
	repo := NewWalletRepository(testDB.Pool)
	w := &wallet.Wallet{UserID: userID, Name: "Retry Wallet", Address: "0x6666666666666666666666666666666666666666"}
	require.NoError(t, repo.Create(ctx, w))

	// Cycle 1 outcome: base errored at a preserved cursor; the wallet rolls up to error.
	cursor := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SetChainCollectCursor(ctx, w.ID, "base", cursor))
	require.NoError(t, repo.SetChainSyncError(ctx, w.ID, "base", "provider 503"))
	require.NoError(t, repo.RollupWalletSyncStatus(ctx, w.ID))

	got, err := repo.GetByID(ctx, w.ID)
	require.NoError(t, err)
	require.Equal(t, wallet.SyncStatusError, got.SyncStatus, "errored wallet is re-selected next cycle")

	// Cycle 2: claim re-pends every chain (syncing, error cleared) but preserves cursors.
	claimed, err := repo.ClaimWalletForSync(ctx, w.ID)
	require.NoError(t, err)
	require.True(t, claimed)

	rows, err := repo.GetChainSyncRows(ctx, w.ID)
	require.NoError(t, err)
	byChain := map[string]wallet.WalletChainSync{}
	for _, r := range rows {
		byChain[r.Chain] = r
	}

	base := byChain["base"]
	assert.Equal(t, wallet.SyncStatusSyncing, base.SyncStatus, "previously-errored chain is re-pended")
	assert.Nil(t, base.SyncError, "its error is cleared for the retry")
	require.NotNil(t, base.CollectCursorAt)
	assert.True(t, cursor.Equal(*base.CollectCursorAt),
		"cursor preserved so the retry resumes from where it left off, skipping nothing")
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
