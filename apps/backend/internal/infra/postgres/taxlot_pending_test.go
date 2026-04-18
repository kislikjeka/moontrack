//go:build integration

package postgres

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/stretchr/testify/require"
)

// seedUserAndWallet inserts a minimal user + wallet using the current schema
// (wallets.chain_id was dropped in migration 000011).
func seedUserAndWallet(t *testing.T, ctx context.Context) (userID, walletID uuid.UUID) {
	t.Helper()

	userID = uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'hash', NOW(), NOW())
	`, userID, fmt.Sprintf("taxlot-%s@test.com", userID.String()[:8]))
	require.NoError(t, err)

	walletID = uuid.New()
	addr := fmt.Sprintf("0x%032x", [16]byte(walletID))
	_, err = testDB.Pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, address, sync_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', NOW(), NOW())
	`, walletID, userID, "Wallet "+walletID.String()[:8], addr)
	require.NoError(t, err)

	return userID, walletID
}

// seedAccountForPendingTest creates a minimal crypto_wallet account.
func seedAccountForPendingTest(t *testing.T, ctx context.Context, walletID uuid.UUID, asset string) uuid.UUID {
	t.Helper()
	accountID := uuid.New()
	code := fmt.Sprintf("wallet.%s.%s", walletID.String(), asset)
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO accounts (id, code, type, asset_id, wallet_id, created_at)
		VALUES ($1, $2, 'CRYPTO_WALLET', $3, $4, NOW())
	`, accountID, code, asset, walletID)
	require.NoError(t, err)
	return accountID
}

// seedTransactionForPendingTest creates a minimal transaction row.
func seedTransactionForPendingTest(t *testing.T, ctx context.Context, walletID uuid.UUID) uuid.UUID {
	t.Helper()
	txID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO transactions (
			id, type, source, external_id, wallet_id,
			status, version, occurred_at, recorded_at, raw_data
		) VALUES ($1, 'transfer_in', 'zerion', $2, $3, 'COMPLETED', 1, NOW(), NOW(), '{}'::jsonb)
	`, txID, txID.String(), walletID)
	require.NoError(t, err)
	return txID
}

// seedPendingLot seeds a tax lot with PriceStatusPending and nil AutoCostBasisPerUnit.
func seedPendingLot(t *testing.T, ctx context.Context, repo *TaxLotRepository, walletID uuid.UUID, asset string, at time.Time) *ledger.TaxLot {
	t.Helper()
	accountID := seedAccountForPendingTest(t, ctx, walletID, asset)
	txID := seedTransactionForPendingTest(t, ctx, walletID)

	lot := &ledger.TaxLot{
		ID:                      uuid.New(),
		TransactionID:           txID,
		AccountID:               accountID,
		Asset:                   asset,
		QuantityAcquired:        big.NewInt(1_000_000_000),
		QuantityRemaining:       big.NewInt(1_000_000_000),
		AcquiredAt:              at,
		AutoCostBasisPerUnit:    nil, // pending — no price yet
		AutoCostBasisSource:     ledger.CostBasisFMVAtTransfer,
		PriceStatus:             ledger.PriceStatusPending,
		PriceResolutionAttempts: 0,
		CreatedAt:               time.Now().UTC(),
	}
	require.NoError(t, repo.CreateTaxLot(ctx, lot))
	return lot
}

func TestTaxLotRepo_ListPendingLots(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	at := time.Now().UTC().Truncate(time.Minute)
	lot := seedPendingLot(t, ctx, repo, walletID, "ETH", at)

	// Must find the lot within the minute bucket
	lots, err := repo.ListPendingLotsByAssetAndTime(ctx, "ETH", at)
	require.NoError(t, err)
	require.Len(t, lots, 1)
	require.Equal(t, lot.ID, lots[0].ID)
	require.Equal(t, ledger.PriceStatusPending, lots[0].PriceStatus)
	require.Nil(t, lots[0].AutoCostBasisPerUnit, "pending lot must have nil cost basis")

	// ResolvePendingPrice transitions to resolved and sets cost basis
	require.NoError(t, repo.ResolvePendingPrice(ctx, lot.ID, big.NewInt(100_000_000), ledger.CostBasisFMVAtTransfer))

	resolved, err := repo.GetTaxLot(ctx, lot.ID)
	require.NoError(t, err)
	require.Equal(t, ledger.PriceStatusResolved, resolved.PriceStatus)
	require.NotNil(t, resolved.AutoCostBasisPerUnit)
	require.Equal(t, "100000000", resolved.EffectiveCostBasisPerUnit().String())

	// After resolution, listing pending lots for the same minute should return 0
	afterResolve, err := repo.ListPendingLotsByAssetAndTime(ctx, "ETH", at)
	require.NoError(t, err)
	require.Len(t, afterResolve, 0)
}

func TestTaxLotRepo_MarkUnpriceable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	lot := seedPendingLot(t, ctx, repo, walletID, "XTKN", time.Now().UTC())

	require.NoError(t, repo.MarkUnpriceable(ctx, lot.ID))

	fetched, err := repo.GetTaxLot(ctx, lot.ID)
	require.NoError(t, err)
	require.Equal(t, ledger.PriceStatusUnpriceable, fetched.PriceStatus)
	// MarkUnpriceable only works on pending rows; calling again is a no-op
	require.NoError(t, repo.MarkUnpriceable(ctx, lot.ID))
}

func TestTaxLotRepo_IncrementAttempt(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	lot := seedPendingLot(t, ctx, repo, walletID, "YTKN", time.Now().UTC())

	nextRetry := time.Now().UTC().Add(15 * time.Minute).Truncate(time.Second)
	require.NoError(t, repo.IncrementAttempt(ctx, lot.ID, 1, nextRetry))

	fetched, err := repo.GetTaxLot(ctx, lot.ID)
	require.NoError(t, err)
	require.Equal(t, ledger.PriceStatusPending, fetched.PriceStatus, "status must remain pending after increment")
	require.Equal(t, 1, fetched.PriceResolutionAttempts)
	require.NotNil(t, fetched.PriceNextRetryAt)
	require.WithinDuration(t, nextRetry, *fetched.PriceNextRetryAt, time.Second)
}

// TestTaxLotRepo_ResolvePendingDisposals verifies end-to-end that:
//  1. CreateDisposal accepts a nil ProceedsPerUnit with ProceedsStatusPending.
//  2. ResolvePendingDisposals fills in proceeds_per_unit and flips the status,
//     scoped by the asset UUID so cross-chain collisions cannot leak.
func TestTaxLotRepo_ResolvePendingDisposals(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	// Create an asset row that matches the seeded account's (symbol, chain_id)
	// so ResolvePendingDisposals can resolve via the JOIN.
	assetID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO assets (id, symbol, name, coingecko_id, decimals, asset_type, chain_id)
		VALUES ($1, 'TKN', 'Token', NULL, 8, 'crypto', NULL)
	`, assetID)
	require.NoError(t, err)

	// Seed a lot the disposal can reference.
	accountID := seedAccountForPendingTest(t, ctx, walletID, "TKN")
	txID := seedTransactionForPendingTest(t, ctx, walletID)

	lot := &ledger.TaxLot{
		ID:                   uuid.New(),
		TransactionID:        txID,
		AccountID:            accountID,
		Asset:                "TKN",
		QuantityAcquired:     big.NewInt(1_000),
		QuantityRemaining:    big.NewInt(500),
		AcquiredAt:           time.Now().UTC().Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(50_000_000),
		AutoCostBasisSource:  ledger.CostBasisFMVAtTransfer,
		PriceStatus:          ledger.PriceStatusResolved,
		CreatedAt:            time.Now().UTC(),
	}
	require.NoError(t, repo.CreateTaxLot(ctx, lot))

	disposedAt := time.Now().UTC().Truncate(time.Minute)
	disposal := &ledger.LotDisposal{
		ID:               uuid.New(),
		TransactionID:    txID,
		LotID:            lot.ID,
		QuantityDisposed: big.NewInt(500),
		ProceedsPerUnit:  nil, // pending — no price at disposal time
		ProceedsStatus:   ledger.ProceedsStatusPending,
		DisposalType:     ledger.DisposalTypeSale,
		DisposedAt:       disposedAt,
		CreatedAt:        time.Now().UTC(),
	}
	require.NoError(t, repo.CreateDisposal(ctx, disposal))

	// Before resolution — the fetched disposal must have nil proceeds and pending status.
	fetched, err := repo.GetDisposalsByTransaction(ctx, txID)
	require.NoError(t, err)
	require.Len(t, fetched, 1)
	require.Nil(t, fetched[0].ProceedsPerUnit)
	require.Equal(t, ledger.ProceedsStatusPending, fetched[0].ProceedsStatus)

	// Resolve.
	n, err := repo.ResolvePendingDisposals(ctx, assetID, disposedAt, big.NewInt(75_000_000))
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	// After resolution.
	fetched, err = repo.GetDisposalsByTransaction(ctx, txID)
	require.NoError(t, err)
	require.Len(t, fetched, 1)
	require.NotNil(t, fetched[0].ProceedsPerUnit)
	require.Equal(t, "75000000", fetched[0].ProceedsPerUnit.String())
	require.Equal(t, ledger.ProceedsStatusResolved, fetched[0].ProceedsStatus)

	// A second call is a no-op (no pending disposals left).
	n, err = repo.ResolvePendingDisposals(ctx, assetID, disposedAt, big.NewInt(123))
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
}

// TestTaxLotRepo_ClearOverride_RefusesWhenAutoIsNull verifies that clearing an
// override on a lot whose auto_cost_basis_per_unit is NULL returns
// ErrCannotClearOverrideOnPendingAuto (instead of silently leaving the lot
// with no effective cost basis and a non-pending status).
func TestTaxLotRepo_ClearOverride_RefusesWhenAutoIsNull(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	// Seed a pending lot (auto NULL), then apply an override and flip to resolved.
	lot := seedPendingLot(t, ctx, repo, walletID, "WWW", time.Now().UTC())
	require.NoError(t, repo.UpdateOverride(ctx, lot.ID, big.NewInt(123_000_000), "manual price"))
	// Move status off 'pending' to simulate the landmine: override present, auto NULL,
	// but status is no longer pending.
	require.NoError(t, repo.MarkResolved(ctx, lot.ID))

	err := repo.ClearOverride(ctx, lot.ID)
	require.ErrorIs(t, err, ledger.ErrCannotClearOverrideOnPendingAuto, "ClearOverride must refuse when auto is NULL")

	// Override should still be present since the clear was refused.
	fetched, err := repo.GetTaxLot(ctx, lot.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched.OverrideCostBasisPerUnit, "override must remain intact after refusal")
}

// TestTaxLotRepo_ClearOverride_AllowedWhenAutoResolved verifies the happy path:
// when a lot has a valid auto cost basis, ClearOverride removes the override.
func TestTaxLotRepo_ClearOverride_AllowedWhenAutoResolved(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	accountID := seedAccountForPendingTest(t, ctx, walletID, "QQQ")
	txID := seedTransactionForPendingTest(t, ctx, walletID)

	lot := &ledger.TaxLot{
		ID:                   uuid.New(),
		TransactionID:        txID,
		AccountID:            accountID,
		Asset:                "QQQ",
		QuantityAcquired:     big.NewInt(1_000),
		QuantityRemaining:    big.NewInt(1_000),
		AcquiredAt:           time.Now().UTC(),
		AutoCostBasisPerUnit: big.NewInt(100_000_000),
		AutoCostBasisSource:  ledger.CostBasisFMVAtTransfer,
		PriceStatus:          ledger.PriceStatusResolved,
		CreatedAt:            time.Now().UTC(),
	}
	require.NoError(t, repo.CreateTaxLot(ctx, lot))
	require.NoError(t, repo.UpdateOverride(ctx, lot.ID, big.NewInt(250_000_000), "manual"))

	require.NoError(t, repo.ClearOverride(ctx, lot.ID))

	fetched, err := repo.GetTaxLot(ctx, lot.ID)
	require.NoError(t, err)
	require.Nil(t, fetched.OverrideCostBasisPerUnit, "override must be cleared")
	require.NotNil(t, fetched.AutoCostBasisPerUnit, "auto cost basis must remain")
}

// TestTaxLotRepo_GetWAC_MixedPendingAndResolved verifies that GetWAC does not
// fail when some lots are pending (cost basis NULL in the materialized view).
// Before the fix, scanning a NULL weighted_avg_cost into a string panicked.
func TestTaxLotRepo_GetWAC_MixedPendingAndResolved(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	// Seed one resolved + one pending lot for the same (account, asset).
	accountID := seedAccountForPendingTest(t, ctx, walletID, "ETH")
	txID := seedTransactionForPendingTest(t, ctx, walletID)

	resolvedLot := &ledger.TaxLot{
		ID:                   uuid.New(),
		TransactionID:        txID,
		AccountID:            accountID,
		Asset:                "ETH",
		QuantityAcquired:     big.NewInt(1_000),
		QuantityRemaining:    big.NewInt(1_000),
		AcquiredAt:           time.Now().UTC().Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(200_000_000), // $2
		AutoCostBasisSource:  ledger.CostBasisFMVAtTransfer,
		PriceStatus:          ledger.PriceStatusResolved,
		CreatedAt:            time.Now().UTC(),
	}
	require.NoError(t, repo.CreateTaxLot(ctx, resolvedLot))

	pendingTxID := seedTransactionForPendingTest(t, ctx, walletID)
	pendingLot := &ledger.TaxLot{
		ID:                      uuid.New(),
		TransactionID:           pendingTxID,
		AccountID:               accountID,
		Asset:                   "ETH",
		QuantityAcquired:        big.NewInt(500),
		QuantityRemaining:       big.NewInt(500),
		AcquiredAt:              time.Now().UTC().Truncate(time.Minute),
		AutoCostBasisPerUnit:    nil, // pending — NULL in DB
		AutoCostBasisSource:     ledger.CostBasisFMVAtTransfer,
		PriceStatus:             ledger.PriceStatusPending,
		PriceResolutionAttempts: 0,
		CreatedAt:               time.Now().UTC(),
	}
	require.NoError(t, repo.CreateTaxLot(ctx, pendingLot))

	// Refresh the materialized view so the new lots are visible.
	require.NoError(t, repo.RefreshWAC(ctx))

	positions, err := repo.GetWAC(ctx, []uuid.UUID{accountID})
	require.NoError(t, err, "GetWAC must not fail when a pending lot yields NULL weighted_avg_cost")
	require.Len(t, positions, 1, "one (account, asset) position expected")

	p := positions[0]
	require.Equal(t, accountID, p.AccountID)
	require.Equal(t, "ETH", p.Asset)
	require.NotNil(t, p.TotalQuantity, "TotalQuantity must always be populated")
	// Either WAC is resolved from the non-pending lots, or nil — both are
	// acceptable. What must NOT happen is a scan failure or a panic.
	if p.WeightedAvgCost != nil {
		require.Equal(t, 1, p.WeightedAvgCost.Sign(), "resolved WAC must be positive")
	}
}

// TestTaxLotRepo_GetWAC_MixedPending_ExcludesPendingFromDenominator verifies
// that migration 000027 prevents WAC deflation for mixed positions: seed one
// resolved lot (qty=100, cost=$10 at 10^8 = 1_000_000_000) and one pending
// lot (qty=100, no cost); without the fix, Postgres SUM would skip the pending
// lot's NULL cost in the numerator but include its quantity in the denominator,
// yielding WAC = 1_000_000_000 * 100 / 200 = 500_000_000 ($5). After the fix,
// the resolved-only CTE yields WAC = 1_000_000_000 * 100 / 100 = 1_000_000_000 ($10).
func TestTaxLotRepo_GetWAC_MixedPending_ExcludesPendingFromDenominator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	accountID := seedAccountForPendingTest(t, ctx, walletID, "MIX")

	// Resolved lot: qty=100, cost per unit = 10 * 10^8
	resolvedTxID := seedTransactionForPendingTest(t, ctx, walletID)
	resolvedLot := &ledger.TaxLot{
		ID:                   uuid.New(),
		TransactionID:        resolvedTxID,
		AccountID:            accountID,
		Asset:                "MIX",
		QuantityAcquired:     big.NewInt(100),
		QuantityRemaining:    big.NewInt(100),
		AcquiredAt:           time.Now().UTC().Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(1_000_000_000), // $10 scaled 10^8
		AutoCostBasisSource:  ledger.CostBasisFMVAtTransfer,
		PriceStatus:          ledger.PriceStatusResolved,
		CreatedAt:            time.Now().UTC(),
	}
	require.NoError(t, repo.CreateTaxLot(ctx, resolvedLot))

	// Pending lot: qty=100, no cost
	pendingTxID := seedTransactionForPendingTest(t, ctx, walletID)
	pendingLot := &ledger.TaxLot{
		ID:                      uuid.New(),
		TransactionID:           pendingTxID,
		AccountID:               accountID,
		Asset:                   "MIX",
		QuantityAcquired:        big.NewInt(100),
		QuantityRemaining:       big.NewInt(100),
		AcquiredAt:              time.Now().UTC().Truncate(time.Minute),
		AutoCostBasisPerUnit:    nil,
		AutoCostBasisSource:     ledger.CostBasisFMVAtTransfer,
		PriceStatus:             ledger.PriceStatusPending,
		PriceResolutionAttempts: 0,
		CreatedAt:               time.Now().UTC(),
	}
	require.NoError(t, repo.CreateTaxLot(ctx, pendingLot))

	require.NoError(t, repo.RefreshWAC(ctx))

	positions, err := repo.GetWAC(ctx, []uuid.UUID{accountID})
	require.NoError(t, err)
	require.Len(t, positions, 1, "one (account, asset) position expected")

	p := positions[0]
	require.Equal(t, accountID, p.AccountID)
	require.Equal(t, "MIX", p.Asset)

	// Total quantity spans every open lot (pending + resolved) since the
	// pending quantity is still part of the user's holdings.
	require.Equal(t, "200", p.TotalQuantity.String(), "total_quantity should include every open lot")

	// WAC must reflect only the resolved lot's cost; the pending lot must
	// NOT deflate the denominator.
	require.NotNil(t, p.WeightedAvgCost, "WAC should be resolved since at least one lot has a cost basis")
	require.Equal(t, "1000000000", p.WeightedAvgCost.String(),
		"WAC should equal resolved cost ($10 scaled 10^8), not $5 which would indicate pending quantity leaking into the denominator")
}

// TestTaxLotRepo_GetWAC_AllPending_NilWAC verifies that when every lot is
// pending, GetWAC still returns a row but with WeightedAvgCost == nil.
func TestTaxLotRepo_GetWAC_AllPending_NilWAC(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)

	lot := seedPendingLot(t, ctx, repo, walletID, "ZZZ", time.Now().UTC())
	require.NoError(t, repo.RefreshWAC(ctx))

	positions, err := repo.GetWAC(ctx, []uuid.UUID{lot.AccountID})
	require.NoError(t, err, "GetWAC must tolerate NULL weighted_avg_cost")
	require.Len(t, positions, 1)
	require.Nil(t, positions[0].WeightedAvgCost, "WAC must be nil when all lots are pending")
}

// TestTaxLotRepo_CreateTaxLot_DefaultsToResolved verifies backward compat:
// existing callers that don't set PriceStatus get 'resolved'.
func TestTaxLotRepo_CreateTaxLot_DefaultsToResolved(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewTaxLotRepository(testDB.Pool)
	_, walletID := seedUserAndWallet(t, ctx)
	accountID := seedAccountForPendingTest(t, ctx, walletID, "BTC")
	txID := seedTransactionForPendingTest(t, ctx, walletID)

	// Simulate a caller that does NOT set PriceStatus (zero value "")
	lot := &ledger.TaxLot{
		ID:                   uuid.New(),
		TransactionID:        txID,
		AccountID:            accountID,
		Asset:                "BTC",
		QuantityAcquired:     big.NewInt(1_000_000),
		QuantityRemaining:    big.NewInt(1_000_000),
		AcquiredAt:           time.Now().UTC(),
		AutoCostBasisPerUnit: big.NewInt(5_000_000_000), // $50,000
		AutoCostBasisSource:  ledger.CostBasisFMVAtTransfer,
		// PriceStatus intentionally omitted (zero value "")
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.CreateTaxLot(ctx, lot))

	fetched, err := repo.GetTaxLot(ctx, lot.ID)
	require.NoError(t, err)
	require.Equal(t, ledger.PriceStatusResolved, fetched.PriceStatus, "zero-value PriceStatus must default to 'resolved'")
	require.NotNil(t, fetched.AutoCostBasisPerUnit)
	require.Equal(t, "5000000000", fetched.AutoCostBasisPerUnit.String())
}
