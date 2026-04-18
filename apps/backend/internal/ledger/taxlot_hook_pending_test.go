package ledger

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
)

// makeEntryNilRate is like makeEntry but with USDRate set to nil,
// simulating an acquisition where no price was available.
func makeEntryNilRate(accountID uuid.UUID, dc DebitCredit, et EntryType, amount int64, asset string) *Entry {
	return &Entry{
		ID:          uuid.New(),
		AccountID:   accountID,
		DebitCredit: dc,
		EntryType:   et,
		Amount:      big.NewInt(amount),
		AssetID:     asset,
		USDRate:     nil, // no price available
		USDValue:    big.NewInt(0),
		OccurredAt:  time.Now(),
		CreatedAt:   time.Now(),
	}
}

// TestTaxLotHook_CreatesPendingLot verifies that when an acquisition entry has
// USDRate == nil, the hook creates a lot with PriceStatus == PriceStatusPending
// and AutoCostBasisPerUnit == nil (not defaulting to zero).
func TestTaxLotHook_CreatesPendingLot(t *testing.T) {
	walletAcctID := uuid.New()
	incomeAcctID := uuid.New()

	taxLotRepo := &mockTaxLotRepo{}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID: walletAccount(walletAcctID),
		incomeAcctID: incomeAccount(incomeAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeTransferIn,
		Entries: []*Entry{
			makeEntryNilRate(walletAcctID, Debit, EntryTypeAssetIncrease, 5000, "ETH"),
			makeEntryNilRate(incomeAcctID, Credit, EntryTypeIncome, 5000, "ETH"),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.lots) != 1 {
		t.Fatalf("expected 1 lot, got %d", len(taxLotRepo.lots))
	}

	lot := taxLotRepo.lots[0]

	// Quantity must be set correctly regardless of price status.
	if lot.QuantityAcquired.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("expected QuantityAcquired 5000, got %s", lot.QuantityAcquired)
	}
	if lot.QuantityRemaining.Cmp(big.NewInt(5000)) != 0 {
		t.Errorf("expected QuantityRemaining 5000, got %s", lot.QuantityRemaining)
	}

	// AutoCostBasisPerUnit must be nil — not zero — when no price was available.
	if lot.AutoCostBasisPerUnit != nil {
		t.Errorf("expected AutoCostBasisPerUnit to be nil (pending), got %s", lot.AutoCostBasisPerUnit)
	}

	// PriceStatus must be pending.
	if lot.PriceStatus != PriceStatusPending {
		t.Errorf("expected PriceStatus %q, got %q", PriceStatusPending, lot.PriceStatus)
	}
}

// TestTaxLotHook_DisposalWithoutRate_CreatesPendingDisposal verifies that when
// an asset is disposed of without a USD rate on the ledger entry, the hook
// creates a LotDisposal with ProceedsStatus=pending and ProceedsPerUnit=nil
// instead of silently freezing proceeds at $0.
func TestTaxLotHook_DisposalWithoutRate_CreatesPendingDisposal(t *testing.T) {
	walletAcctID := uuid.New()
	expenseAcctID := uuid.New()

	// Pre-seed a lot on the wallet.
	existingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            walletAcctID,
		Asset:                "ETH",
		QuantityAcquired:     big.NewInt(1000),
		QuantityRemaining:    big.NewInt(1000),
		AcquiredAt:           time.Now().Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(100_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            time.Now(),
	}

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{existingLot}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID:  walletAccount(walletAcctID),
		expenseAcctID: expenseAccount(expenseAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeTransferOut,
		Entries: []*Entry{
			makeEntryNilRate(expenseAcctID, Debit, EntryTypeExpense, 500, "ETH"),
			makeEntryNilRate(walletAcctID, Credit, EntryTypeAssetDecrease, 500, "ETH"),
		},
	}

	if err := hook(context.Background(), tx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.disposals) != 1 {
		t.Fatalf("expected 1 disposal, got %d", len(taxLotRepo.disposals))
	}
	d := taxLotRepo.disposals[0]

	if d.ProceedsPerUnit != nil {
		t.Errorf("expected nil ProceedsPerUnit for pending disposal, got %s", d.ProceedsPerUnit)
	}
	if d.ProceedsStatus != ProceedsStatusPending {
		t.Errorf("expected ProceedsStatus %q, got %q", ProceedsStatusPending, d.ProceedsStatus)
	}
	if d.QuantityDisposed.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("expected QuantityDisposed 500, got %s", d.QuantityDisposed)
	}
}

// TestWeightedAvgCostBasis_SkipsPendingLots verifies that when weightedAvgCostBasis
// encounters a disposal consuming a pending source lot (EffectiveCostBasisPerUnit()
// returns nil), it does not panic on Mul(nil, ...) and computes the WAC from the
// resolved lots only.
func TestWeightedAvgCostBasis_SkipsPendingLots(t *testing.T) {
	accountID := uuid.New()
	asset := "ETH"
	now := time.Now()

	// One resolved source lot, one pending source lot.
	resolvedLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            accountID,
		Asset:                asset,
		QuantityAcquired:     big.NewInt(100),
		QuantityRemaining:    big.NewInt(100),
		AcquiredAt:           now.Add(-2 * time.Hour),
		AutoCostBasisPerUnit: big.NewInt(200_000_000), // $2 scaled 10^8
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            now,
	}
	pendingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            accountID,
		Asset:                asset,
		QuantityAcquired:     big.NewInt(100),
		QuantityRemaining:    big.NewInt(100),
		AcquiredAt:           now.Add(-time.Hour),
		AutoCostBasisPerUnit: nil, // pending — unresolved
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusPending,
		CreatedAt:            now,
	}

	repo := &mockTaxLotRepo{lots: []*TaxLot{resolvedLot, pendingLot}}

	// Disposals consuming 50 units from each lot.
	disposals := []*LotDisposal{
		{
			ID:               uuid.New(),
			TransactionID:    uuid.New(),
			LotID:            resolvedLot.ID,
			QuantityDisposed: big.NewInt(50),
			ProceedsPerUnit:  big.NewInt(300_000_000),
			DisposalType:     DisposalTypeInternalTransfer,
			DisposedAt:       now,
			CreatedAt:        now,
		},
		{
			ID:               uuid.New(),
			TransactionID:    uuid.New(),
			LotID:            pendingLot.ID,
			QuantityDisposed: big.NewInt(50),
			ProceedsPerUnit:  big.NewInt(300_000_000),
			DisposalType:     DisposalTypeInternalTransfer,
			DisposedAt:       now,
			CreatedAt:        now,
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("weightedAvgCostBasis panicked on pending lot: %v", r)
		}
	}()

	wac := weightedAvgCostBasis(context.Background(), repo, disposals)
	if wac == nil {
		t.Fatalf("expected WAC from resolved lot, got nil")
	}
	// WAC should equal the resolved lot's cost basis (50*$2 / 50 = $2)
	expected := big.NewInt(200_000_000)
	if wac.Cmp(expected) != 0 {
		t.Errorf("expected WAC %s (resolved lot only), got %s", expected, wac)
	}
}

// TestWeightedAvgCostBasis_AllPending_ReturnsNil verifies that when every disposal
// references a pending lot, the function returns nil (so callers fall back to FMV).
func TestWeightedAvgCostBasis_AllPending_ReturnsNil(t *testing.T) {
	accountID := uuid.New()
	asset := "ETH"
	now := time.Now()

	pendingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            accountID,
		Asset:                asset,
		QuantityAcquired:     big.NewInt(100),
		QuantityRemaining:    big.NewInt(100),
		AcquiredAt:           now.Add(-time.Hour),
		AutoCostBasisPerUnit: nil,
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusPending,
		CreatedAt:            now,
	}

	repo := &mockTaxLotRepo{lots: []*TaxLot{pendingLot}}

	disposals := []*LotDisposal{
		{
			ID:               uuid.New(),
			TransactionID:    uuid.New(),
			LotID:            pendingLot.ID,
			QuantityDisposed: big.NewInt(50),
			ProceedsPerUnit:  big.NewInt(300_000_000),
			DisposalType:     DisposalTypeInternalTransfer,
			DisposedAt:       now,
			CreatedAt:        now,
		},
	}

	wac := weightedAvgCostBasis(context.Background(), repo, disposals)
	if wac != nil {
		t.Fatalf("expected nil WAC when all source lots are pending, got %s", wac)
	}
}

// TestTaxLotHook_ResolvedWhenRatePresent verifies that when USDRate is non-nil
// the hook creates a lot with PriceStatus == PriceStatusResolved (regression guard).
func TestTaxLotHook_ResolvedWhenRatePresent(t *testing.T) {
	walletAcctID := uuid.New()
	incomeAcctID := uuid.New()

	taxLotRepo := &mockTaxLotRepo{}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID: walletAccount(walletAcctID),
		incomeAcctID: incomeAccount(incomeAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeTransferIn,
		Entries: []*Entry{
			makeEntry(walletAcctID, Debit, EntryTypeAssetIncrease, 1000, "ETH", nil),
			makeEntry(incomeAcctID, Credit, EntryTypeIncome, 1000, "ETH", nil),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.lots) != 1 {
		t.Fatalf("expected 1 lot, got %d", len(taxLotRepo.lots))
	}

	lot := taxLotRepo.lots[0]

	if lot.PriceStatus != PriceStatusResolved {
		t.Errorf("expected PriceStatus %q, got %q", PriceStatusResolved, lot.PriceStatus)
	}

	if lot.AutoCostBasisPerUnit == nil {
		t.Error("expected AutoCostBasisPerUnit to be non-nil when rate is present")
	}
}
