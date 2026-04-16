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
