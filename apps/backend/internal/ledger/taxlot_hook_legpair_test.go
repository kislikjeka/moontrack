package ledger

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// =============================================================================
// Issue #84 — the hook follows an explicit leg pair, never an inference.
//
// The old key was asset equality. It survived only while #70 gave the two legs
// of a bridge one UUID, and it never distinguished the GAS leg — which is booked
// as an asset decrease on a wallet account, in the same asset as the principal
// whenever the fee is paid in the coin being moved.
//
// That collision is not hypothetical. The live database already holds collateral
// supplies paid for in the very coin supplied, where gas, principal and
// collateral all carry one UUID. Correctness there rested on FIFO happening to
// consume the same lot twice, which is an accident of ordering: with several
// lots open the link would have gone to the wrong one, the destination lot would
// have inherited the wrong basis, and the transaction would still have balanced.
// =============================================================================

func collateralAccount(id uuid.UUID, asset uuid.UUID) *Account {
	wid := uuid.New()
	chain := "base"
	return &Account{
		ID:       id,
		Code:     "collateral.aave." + wid.String() + "." + chain + "." + asset.String(),
		Type:     AccountTypeCollateral,
		AssetID:  asset,
		WalletID: &wid,
		ChainID:  &chain,
	}
}

// lotAt is an open lot of `qty` acquired `ago` before now at `basis` per unit.
func lotAt(acctID uuid.UUID, asset uuid.UUID, qty int64, basis int64, ago time.Duration) *TaxLot {
	return &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            acctID,
		Asset:                asset,
		QuantityAcquired:     big.NewInt(qty),
		QuantityRemaining:    big.NewInt(qty),
		AcquiredAt:           time.Now().Add(-ago),
		AutoCostBasisPerUnit: big.NewInt(basis),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            time.Now(),
	}
}

// TestTaxLotHook_CollateralSupply_GasInSameAssetCannotStealTheLink is the
// production shape the marker closes.
//
// A wallet supplies 1000 units of the native coin as collateral and pays the fee
// in that same coin. Two lots are open, so FIFO gives the gas disposal the OLD
// cheap lot and the principal disposal the NEW expensive one. Every property an
// inference could key on is identical between the two disposals — same asset,
// same account, same chain, same entry type — so only the pair marker can say
// which one the collateral lot inherits from.
//
// The gas leg is deliberately listed FIRST, because that is the ordering that
// makes an unmarked hook pick the wrong disposal: whichever registers first for
// the asset becomes the link.
func TestTaxLotHook_CollateralSupply_GasInSameAssetCannotStealTheLink(t *testing.T) {
	walletAcctID := uuid.New()
	collAcctID := uuid.New()

	// FIFO order: the $10 lot is consumed first, by whichever disposal runs
	// first. The $90 lot is what the principal actually gives up.
	gasLot := lotAt(walletAcctID, testasset.ETH, 5, 10_000_000, 48*time.Hour)       // $0.10/unit
	principalLot := lotAt(walletAcctID, testasset.ETH, 1000, 90_000_000, time.Hour) // $0.90/unit

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{gasLot, principalLot}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID: chainWalletAccount(walletAcctID, "base", testasset.ETH),
		collAcctID:   collateralAccount(collAcctID, testasset.ETH),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	const pair = "lending:0xsupply:eth"
	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeLendingSupply,
		Entries: []*Entry{
			// Gas first, and unmarked: it is nobody's counterpart.
			makeEntry(walletAcctID, Credit, EntryTypeAssetDecrease, 5, testasset.ETH, map[string]interface{}{
				"chain_id":   "base",
				"entry_type": "gas_payment",
			}),
			// The principal pair.
			makeEntry(collAcctID, Debit, EntryTypeCollateralIncrease, 1000, testasset.ETH, map[string]interface{}{
				"chain_id":  "base",
				MetaLegPair: pair,
			}),
			makeEntry(walletAcctID, Credit, EntryTypeAssetDecrease, 1000, testasset.ETH, map[string]interface{}{
				"chain_id":  "base",
				MetaLegPair: pair,
			}),
		},
	}

	if err := hook(context.Background(), tx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both disposals happened: the fee and the principal.
	if len(taxLotRepo.disposals) != 2 {
		t.Fatalf("expected a gas disposal and a principal disposal, got %d", len(taxLotRepo.disposals))
	}

	// The collateral lot is the only lot created here.
	if len(taxLotRepo.lots) != 3 {
		t.Fatalf("expected the two source lots plus one collateral lot, got %d", len(taxLotRepo.lots))
	}
	collLot := taxLotRepo.lots[2]

	if collLot.LinkedSourceLotID == nil {
		t.Fatal("the collateral lot must link back to the lot the principal consumed")
	}
	if *collLot.LinkedSourceLotID == gasLot.ID {
		t.Fatal("the collateral lot linked to the GAS lot: the fee's disposal was offered as the " +
			"principal's counterpart. Nothing but the leg-pair marker separates them — same asset, " +
			"same account, same chain, same entry type")
	}
	if *collLot.LinkedSourceLotID != principalLot.ID {
		t.Errorf("expected the link to the principal's lot %s, got %s", principalLot.ID, *collLot.LinkedSourceLotID)
	}

	// And the basis follows the link, which is the consequence that reaches the
	// user's PnL: $0.90 from the lot actually supplied, not $0.10 from the fee's.
	if collLot.AutoCostBasisPerUnit.Cmp(big.NewInt(90_000_000)) != 0 {
		t.Errorf("expected the principal lot's basis ($0.90/unit) to carry, got %s",
			collLot.AutoCostBasisPerUnit)
	}
}

// TestTaxLotHook_UnmarkedTransfer_DoesNotCarryBasis: an acquisition that carries
// no marker finds no disposal and opens at FMV. This is what keeps the hook from
// pairing things that were never paired — a sale's proceeds, an airdrop landing
// in a transaction that also spends the same coin.
func TestTaxLotHook_UnmarkedTransfer_DoesNotCarryBasis(t *testing.T) {
	srcAcctID := uuid.New()
	dstAcctID := uuid.New()

	sourceLot := lotAt(srcAcctID, testasset.ETH, 1000, 100_000_000, 24*time.Hour)

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{sourceLot}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		srcAcctID: chainWalletAccount(srcAcctID, "base", testasset.ETH),
		dstAcctID: chainWalletAccount(dstAcctID, "base", testasset.ETH),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeInternalTransfer,
		Entries: []*Entry{
			makeEntry(dstAcctID, Debit, EntryTypeAssetIncrease, 1000, testasset.ETH, map[string]interface{}{"chain_id": "base"}),
			makeEntry(srcAcctID, Credit, EntryTypeAssetDecrease, 1000, testasset.ETH, map[string]interface{}{"chain_id": "base"}),
		},
	}

	if err := hook(context.Background(), tx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	destLot := taxLotRepo.lots[1]
	if destLot.LinkedSourceLotID != nil {
		t.Error("an unmarked acquisition must not link to a disposal it was never paired with")
	}
	// makeEntry prices entries at $200/unit; the source lot was $1.
	if destLot.AutoCostBasisPerUnit.Cmp(big.NewInt(200_000_000_00)) != 0 {
		t.Errorf("an unpaired acquisition opens at FMV, got %s", destLot.AutoCostBasisPerUnit)
	}
}
