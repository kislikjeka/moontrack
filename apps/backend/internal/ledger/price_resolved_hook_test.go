package ledger

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPriceResolvedHook_ResolvesPendingLots verifies that the hook finds all
// pending lots for (asset, time-bucket) and transitions them to resolved with
// the supplied price.
//
// Note: downstream disposal recomputation is implicit — LotDisposal rows do not
// cache cost basis; they call EffectiveCostBasisPerUnit() on the lot at read time.
// So resolving the lot is sufficient.
func TestPriceResolvedHook_ResolvesPendingLots(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Minute)

	pendingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            uuid.New(),
		Asset:                "TOKEN",
		QuantityAcquired:     big.NewInt(1000),
		QuantityRemaining:    big.NewInt(1000),
		AcquiredAt:           at,
		AutoCostBasisPerUnit: nil, // pending — no price yet
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusPending,
		CreatedAt:            time.Now(),
	}

	repo := &mockTaxLotRepo{lots: []*TaxLot{pendingLot}}
	hook := NewPriceResolvedHook(repo, newTestLogger())

	assetID := uuid.New()
	price := big.NewInt(123_000_000) // $1.23 scaled 10^8
	err := hook(ctx, assetID, at, price, CostBasisFMVAtTransfer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Re-fetch via GetTaxLot to confirm mutation.
	got, err := repo.GetTaxLot(ctx, pendingLot.ID)
	if err != nil {
		t.Fatalf("failed to re-fetch lot: %v", err)
	}

	if got.PriceStatus != PriceStatusResolved {
		t.Errorf("expected PriceStatus %q, got %q", PriceStatusResolved, got.PriceStatus)
	}
	if got.AutoCostBasisPerUnit == nil {
		t.Fatal("expected AutoCostBasisPerUnit to be non-nil after resolution")
	}
	if got.AutoCostBasisPerUnit.String() != "123000000" {
		t.Errorf("expected AutoCostBasisPerUnit 123000000, got %s", got.AutoCostBasisPerUnit.String())
	}
}

// TestPriceResolvedHook_NoMatchingLots verifies that the hook is a no-op when
// there are no pending lots for the given (asset, time-bucket).
func TestPriceResolvedHook_NoMatchingLots(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC()

	// Lot is resolved already — should not be touched.
	resolvedLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            uuid.New(),
		Asset:                "TOKEN",
		QuantityAcquired:     big.NewInt(500),
		QuantityRemaining:    big.NewInt(500),
		AcquiredAt:           at,
		AutoCostBasisPerUnit: big.NewInt(50_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            time.Now(),
	}

	repo := &mockTaxLotRepo{lots: []*TaxLot{resolvedLot}}
	hook := NewPriceResolvedHook(repo, newTestLogger())

	err := hook(ctx, uuid.New(), at, big.NewInt(99_000_000), CostBasisFMVAtTransfer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original lot should be untouched.
	if resolvedLot.AutoCostBasisPerUnit.Cmp(big.NewInt(50_000_000)) != 0 {
		t.Errorf("resolved lot cost basis should not have changed")
	}
}

// TestPriceResolvedHook_ResolvesPendingDisposals verifies that when a disposal
// was created without a USD rate (proceeds_status='pending'), the hook fills in
// proceeds_per_unit and flips it to 'resolved' after the price is known.
func TestPriceResolvedHook_ResolvesPendingDisposals(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Minute)

	assetID := uuid.New()

	// Seed a lot the disposal references.
	lot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            uuid.New(),
		Asset:                "TOKEN",
		QuantityAcquired:     big.NewInt(1000),
		QuantityRemaining:    big.NewInt(500),
		AcquiredAt:           at.Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(50_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            time.Now(),
	}

	pendingDisposal := &LotDisposal{
		ID:               uuid.New(),
		TransactionID:    uuid.New(),
		LotID:            lot.ID,
		QuantityDisposed: big.NewInt(500),
		ProceedsPerUnit:  nil,
		ProceedsStatus:   ProceedsStatusPending,
		DisposalType:     DisposalTypeSale,
		DisposedAt:       at,
		CreatedAt:        time.Now(),
	}

	repo := &mockTaxLotRepo{
		lots:        []*TaxLot{lot},
		disposals:   []*LotDisposal{pendingDisposal},
		lotAssetIDs: map[uuid.UUID]uuid.UUID{lot.ID: assetID},
	}
	hook := NewPriceResolvedHook(repo, newTestLogger())

	price := big.NewInt(200_000_000) // $2
	if err := hook(ctx, assetID, at, price, CostBasisFMVAtTransfer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pendingDisposal.ProceedsStatus != ProceedsStatusResolved {
		t.Errorf("expected disposal to be resolved, got %q", pendingDisposal.ProceedsStatus)
	}
	if pendingDisposal.ProceedsPerUnit == nil {
		t.Fatal("expected ProceedsPerUnit to be populated after resolution")
	}
	if pendingDisposal.ProceedsPerUnit.Cmp(price) != 0 {
		t.Errorf("expected ProceedsPerUnit %s, got %s", price, pendingDisposal.ProceedsPerUnit)
	}
}

// TestPriceResolvedHook_OnlyTouchesLotsForAssetID verifies that when two pending
// lots share a symbol but map to different asset UUIDs (same token on two
// chains, e.g. USDT on Ethereum vs USDT on BNB), the hook only resolves the
// lot owned by the asset UUID the worker just resolved.
func TestPriceResolvedHook_OnlyTouchesLotsForAssetID(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Minute)

	ethUSDT := uuid.New()
	bnbUSDT := uuid.New()

	lotOnEth := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            uuid.New(),
		Asset:                "USDT",
		QuantityAcquired:     big.NewInt(1_000_000),
		QuantityRemaining:    big.NewInt(1_000_000),
		AcquiredAt:           at,
		AutoCostBasisPerUnit: nil,
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusPending,
		CreatedAt:            time.Now(),
	}
	lotOnBnb := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            uuid.New(),
		Asset:                "USDT",
		QuantityAcquired:     big.NewInt(2_000_000),
		QuantityRemaining:    big.NewInt(2_000_000),
		AcquiredAt:           at,
		AutoCostBasisPerUnit: nil,
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusPending,
		CreatedAt:            time.Now(),
	}

	repo := &mockTaxLotRepo{
		lots: []*TaxLot{lotOnEth, lotOnBnb},
		lotAssetIDs: map[uuid.UUID]uuid.UUID{
			lotOnEth.ID: ethUSDT,
			lotOnBnb.ID: bnbUSDT,
		},
	}
	hook := NewPriceResolvedHook(repo, newTestLogger())

	price := big.NewInt(100_000_000) // $1 scaled 10^8
	if err := hook(ctx, ethUSDT, at, price, CostBasisFMVAtTransfer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotEth, err := repo.GetTaxLot(ctx, lotOnEth.ID)
	if err != nil {
		t.Fatalf("failed to re-fetch eth lot: %v", err)
	}
	if gotEth.PriceStatus != PriceStatusResolved {
		t.Errorf("eth lot should be resolved, got %q", gotEth.PriceStatus)
	}

	gotBnb, err := repo.GetTaxLot(ctx, lotOnBnb.ID)
	if err != nil {
		t.Fatalf("failed to re-fetch bnb lot: %v", err)
	}
	if gotBnb.PriceStatus != PriceStatusPending {
		t.Errorf("bnb lot must remain pending (not same asset UUID), got %q", gotBnb.PriceStatus)
	}
	if gotBnb.AutoCostBasisPerUnit != nil {
		t.Errorf("bnb lot must not have been priced, got %s", gotBnb.AutoCostBasisPerUnit)
	}
}
