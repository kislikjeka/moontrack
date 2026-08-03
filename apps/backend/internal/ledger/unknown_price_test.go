package ledger

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// The tests in taxlot_hook_pending_test.go call the hook directly and have
// always passed, yet on live data 157/157 lots were written 'resolved' with a
// cost basis of exactly 0 and not one lot was ever pending (#74).
//
// The reason the suite could be green while production was wrong is that those
// tests construct an Entry and hand it to the hook without ever validating it.
// Entry.Validate() — which every entry passes through on the real path, from
// LedgerRepository.createEntry — rejected a nil USDRate outright. So the
// pending branch was reachable in a test and unreachable in production, and no
// test asserted on the seam between them.
//
// These tests pin that seam.

// TestEntryValidate_AcceptsNilUSDRate is the direct regression guard: an entry
// whose price is not yet known must survive validation, because that is the
// only way a pending lot can ever be created on the real path.
func TestEntryValidate_AcceptsNilUSDRate(t *testing.T) {
	e := &Entry{
		DebitCredit: Debit,
		EntryType:   EntryTypeAssetIncrease,
		Amount:      big.NewInt(1000),
		AssetID:     testasset.ETH,
		USDRate:     nil, // price not known yet — a backfill job is queued for it
		USDValue:    nil,
		OccurredAt:  time.Now(),
	}

	if err := e.Validate(); err != nil {
		t.Fatalf("an entry with an unknown price must validate, got: %v", err)
	}
}

// TestEntryValidate_StillRejectsNegativeRate confirms the relaxation above did
// not turn off the check it was carved out of. A negative rate is a genuine
// error, distinct from an absent one.
func TestEntryValidate_StillRejectsNegativeRate(t *testing.T) {
	base := func() *Entry {
		return &Entry{
			DebitCredit: Debit,
			EntryType:   EntryTypeAssetIncrease,
			Amount:      big.NewInt(1000),
			AssetID:     testasset.ETH,
			USDRate:     big.NewInt(100_000_000),
			USDValue:    big.NewInt(100_000_000),
			OccurredAt:  time.Now(),
		}
	}

	negRate := base()
	negRate.USDRate = big.NewInt(-1)
	if err := negRate.Validate(); err != ErrNegativeUSDRate {
		t.Errorf("negative USD rate must be rejected, got: %v", err)
	}

	negValue := base()
	negValue.USDValue = big.NewInt(-1)
	if err := negValue.Validate(); err != ErrNegativeUSDValue {
		t.Errorf("negative USD value must be rejected, got: %v", err)
	}
}

// TestUnpricedAcquisition_EndsUpPendingNotResolvedZero walks the two steps that
// were broken as a pair — validation, then the hook — and asserts the outcome
// the live database contradicted: a lot with no price is pending with a nil
// basis, NOT resolved with a basis of zero.
//
// The distinction is the whole point. 'resolved' means "the price is known";
// paired with 0 it asserts the asset was acquired for nothing, which is both
// false and final: ListPendingLotsByAssetAndTime filters on
// price_status='pending', so a lot marked resolved is never revisited and the
// price the backfill worker later fetches has nowhere to go.
func TestUnpricedAcquisition_EndsUpPendingNotResolvedZero(t *testing.T) {
	walletAcctID := uuid.New()
	incomeAcctID := uuid.New()

	entries := []*Entry{
		makeEntryNilRate(walletAcctID, Debit, EntryTypeAssetIncrease, 5000, testasset.ETH),
		makeEntryNilRate(incomeAcctID, Credit, EntryTypeIncome, 5000, testasset.ETH),
	}

	// Step 1: the entries must clear validation, or the hook never runs.
	for i, e := range entries {
		if err := e.Validate(); err != nil {
			t.Fatalf("entry %d with unknown price failed validation: %v", i, err)
		}
	}

	// Step 2: the hook must classify the acquisition as pending.
	taxLotRepo := &mockTaxLotRepo{}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID: walletAccount(walletAcctID),
		incomeAcctID: incomeAccount(incomeAcctID),
	}}
	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{ID: uuid.New(), Type: TxTypeTransferIn, Entries: entries}
	if err := hook(context.Background(), tx); err != nil {
		t.Fatalf("hook failed: %v", err)
	}

	if len(taxLotRepo.lots) != 1 {
		t.Fatalf("expected 1 lot, got %d", len(taxLotRepo.lots))
	}
	lot := taxLotRepo.lots[0]

	if lot.PriceStatus != PriceStatusPending {
		t.Errorf("status must not claim a price that is unknown: got %q, want %q",
			lot.PriceStatus, PriceStatusPending)
	}
	if lot.AutoCostBasisPerUnit != nil {
		t.Errorf("cost basis must be nil while pending, got %s", lot.AutoCostBasisPerUnit)
	}
	if lot.EffectiveCostBasisPerUnit() != nil {
		t.Errorf("effective cost basis must be nil while pending, got %s",
			lot.EffectiveCostBasisPerUnit())
	}
}

// TestUnpricedLotBecomesEligibleForBackfill closes the loop: the pending lot
// created above is exactly the shape PriceResolvedHook looks for, so the price
// the backfill worker fetches actually lands on it.
//
// On live data this loop was open at both ends — 148 prices sat in
// price_history and 177 backfill jobs were marked resolved while updating zero
// lots, because no lot was pending for them to match.
func TestUnpricedLotBecomesEligibleForBackfill(t *testing.T) {
	walletAcctID := uuid.New()
	incomeAcctID := uuid.New()
	acquiredAt := time.Now().UTC().Truncate(time.Minute)

	entries := []*Entry{
		makeEntryNilRate(walletAcctID, Debit, EntryTypeAssetIncrease, 5000, testasset.ETH),
		makeEntryNilRate(incomeAcctID, Credit, EntryTypeIncome, 5000, testasset.ETH),
	}
	for _, e := range entries {
		e.OccurredAt = acquiredAt
	}

	taxLotRepo := &mockTaxLotRepo{}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID: walletAccount(walletAcctID),
		incomeAcctID: incomeAccount(incomeAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())
	tx := &Transaction{ID: uuid.New(), Type: TxTypeTransferIn, Entries: entries}
	if err := hook(context.Background(), tx); err != nil {
		t.Fatalf("hook failed: %v", err)
	}

	// The backfill worker answers with a real price for that moment.
	const priceUSD = 3_500_00000000 // $3500 scaled by 10^8
	resolved := NewPriceResolvedHook(taxLotRepo, newTestLogger())
	err := resolved(context.Background(), testasset.ETH, acquiredAt,
		big.NewInt(priceUSD), CostBasisFMVAtTransfer)
	if err != nil {
		t.Fatalf("price resolved hook failed: %v", err)
	}

	lot := taxLotRepo.lots[0]
	if lot.PriceStatus != PriceStatusResolved {
		t.Fatalf("lot must be resolved once its price is known, got %q", lot.PriceStatus)
	}
	if lot.AutoCostBasisPerUnit == nil || lot.AutoCostBasisPerUnit.Cmp(big.NewInt(priceUSD)) != 0 {
		t.Fatalf("cost basis must be the resolved price %d, got %v",
			priceUSD, lot.AutoCostBasisPerUnit)
	}
}
