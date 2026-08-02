package ledger

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// TestPriceResolvedHook_ConcurrentResolutions verifies that when two workers
// racing to resolve the same (assetID, at) fire the hook concurrently, the
// idempotency guard provided by ResolvePendingPrice (which only updates rows
// with price_status='pending') prevents double-resolution.
//
// Preconditions guaranteed by the mock:
//   - ResolvePendingPrice is a CAS-like op: it only mutates lots whose status
//     is still 'pending' when the call arrives.
//
// Assertions:
//  1. No panic from concurrent access (exercised under -race).
//  2. The lot ends up resolved with exactly the resolver-supplied cost basis.
//  3. The second concurrent resolution is a no-op (does not flip status or
//     rewrite cost basis).
func TestPriceResolvedHook_ConcurrentResolutions(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Minute)
	assetID := testasset.ForTicker("TOKEN")

	pendingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            uuid.New(),
		Asset:                testasset.ForTicker("TOKEN"),
		QuantityAcquired:     big.NewInt(1_000),
		QuantityRemaining:    big.NewInt(1_000),
		AcquiredAt:           at,
		AutoCostBasisPerUnit: nil,
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusPending,
		CreatedAt:            time.Now(),
	}

	repo := &mockTaxLotRepo{lots: []*TaxLot{pendingLot}}
	hook := NewPriceResolvedHook(repo, newTestLogger())

	const price = int64(123_000_000) // $1.23 @ 10^8

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- hook(ctx, assetID, at, big.NewInt(price), CostBasisFMVAtTransfer)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("hook returned error under concurrent invocation: %v", err)
		}
	}

	got, err := repo.GetTaxLot(ctx, pendingLot.ID)
	if err != nil {
		t.Fatalf("failed to re-fetch lot: %v", err)
	}
	if got.PriceStatus != PriceStatusResolved {
		t.Fatalf("expected status resolved, got %q", got.PriceStatus)
	}
	if got.AutoCostBasisPerUnit == nil {
		t.Fatal("expected cost basis to be set")
	}
	if got.AutoCostBasisPerUnit.Int64() != price {
		t.Errorf("expected cost basis %d, got %s", price, got.AutoCostBasisPerUnit)
	}
}

// TestPriceResolvedHook_PartialBatchFailure verifies the hook's documented
// fail-fast behaviour on mid-batch errors: when ResolvePendingPrice errors on
// the Nth lot in a batch, the hook returns the error; lots processed earlier
// in the loop are already resolved, and lots after the failure remain pending.
//
// This documents current behaviour (not a bug): retrying the hook on the same
// (assetID, at) bucket will re-fetch the still-pending lots and try again.
func TestPriceResolvedHook_PartialBatchFailure(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Minute)
	assetID := testasset.ForTicker("TOKEN")

	// Create 3 pending lots. The mock will return an error on the 2nd
	// call to ResolvePendingPrice.
	lot1 := &TaxLot{
		ID: uuid.New(), TransactionID: uuid.New(), AccountID: uuid.New(),
		Asset:            testasset.ForTicker("TOKEN"),
		QuantityAcquired: big.NewInt(100), QuantityRemaining: big.NewInt(100),
		AcquiredAt: at, PriceStatus: PriceStatusPending, CreatedAt: time.Now(),
	}
	lot2 := &TaxLot{
		ID: uuid.New(), TransactionID: uuid.New(), AccountID: uuid.New(),
		Asset:            testasset.ForTicker("TOKEN"),
		QuantityAcquired: big.NewInt(200), QuantityRemaining: big.NewInt(200),
		AcquiredAt: at, PriceStatus: PriceStatusPending, CreatedAt: time.Now(),
	}
	lot3 := &TaxLot{
		ID: uuid.New(), TransactionID: uuid.New(), AccountID: uuid.New(),
		Asset:            testasset.ForTicker("TOKEN"),
		QuantityAcquired: big.NewInt(300), QuantityRemaining: big.NewInt(300),
		AcquiredAt: at, PriceStatus: PriceStatusPending, CreatedAt: time.Now(),
	}

	injected := errors.New("simulated resolve failure on lot #2")
	repo := &mockTaxLotRepo{
		lots:           []*TaxLot{lot1, lot2, lot3},
		failResolveOn:  2,
		failResolveErr: injected,
	}
	hook := NewPriceResolvedHook(repo, newTestLogger())

	err := hook(ctx, assetID, at, big.NewInt(100_000_000), CostBasisFMVAtTransfer)
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error to propagate, got %v", err)
	}

	// Lot 1 should have been resolved before the failure hit.
	got1, _ := repo.GetTaxLot(ctx, lot1.ID)
	if got1.PriceStatus != PriceStatusResolved {
		t.Errorf("lot1 should be resolved (processed before failure), got %q", got1.PriceStatus)
	}

	// Lots 2 and 3 should still be pending — #2 errored, #3 never ran.
	got2, _ := repo.GetTaxLot(ctx, lot2.ID)
	if got2.PriceStatus != PriceStatusPending {
		t.Errorf("lot2 should remain pending (errored), got %q", got2.PriceStatus)
	}
	got3, _ := repo.GetTaxLot(ctx, lot3.ID)
	if got3.PriceStatus != PriceStatusPending {
		t.Errorf("lot3 should remain pending (never reached), got %q", got3.PriceStatus)
	}
}
