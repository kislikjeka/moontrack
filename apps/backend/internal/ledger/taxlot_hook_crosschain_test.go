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
// Issue #32 — tax-lot carry-over across a bridge.
//
// The accounting contract of a cross-chain internal transfer (ADR-0002): the
// source-chain disposal and the destination-chain acquisition are entries of
// ONE ledger transaction, so the hook's existing carry-over path applies with
// no change of its own. Concretely, three things must hold, or the migration
// fails at exactly the metric it exists to protect:
//
//  1. the source lot is consumed with DisposalType=InternalTransfer — the
//     disposal type is what keeps the disposal out of realized PnL
//  2. the destination lot links back to the consumed source lot
//     (LinkedSourceLotID)
//  3. the destination lot's cost basis is the weighted average of the consumed
//     lots, NOT the market price at bridge time
//
// What makes it work across chains is the LEG-PAIR MARKER the emitting handler
// stamps on both legs (see [MetaLegPair]). It used to be asset equality, which
// held only while #70 gave the two legs one UUID: the destination account was
// built from the destination chain and the SOURCE asset's id. Once each leg
// carries its rightful identity the two assets genuinely differ — a token on
// another chain is another contract, another registry row — so asset equality
// would find nothing, the destination lot would open at market price, and the
// balance would still tie out. These tests pin the marker down, and pin down
// that the legs' assets DIFFER, so that a later change cannot quietly restore
// the inference that #70 was made of.
// =============================================================================

// chainWalletAccount is a wallet account on a specific chain. Cross-chain legs
// differ only in the chain segment of the code and the ChainID field — they are
// two distinct accounts holding the same asset.
func chainWalletAccount(id uuid.UUID, chain string, asset uuid.UUID) *Account {
	wid := uuid.New()
	return &Account{
		ID:       id,
		Code:     "wallet.test." + chain + "." + asset.String(),
		Type:     AccountTypeCryptoWallet,
		AssetID:  asset,
		WalletID: &wid,
		ChainID:  &chain,
	}
}

// crossChainTransferTx is a bridge booked as one internal_transfer: the asset
// leaves the source-chain account and arrives on the destination-chain account.
//
// The two legs carry DIFFERENT assets, which is the whole shape of a bridge
// after #84: identity is (chain, contract), so ETH on Base and ETH on Arbitrum
// are two registry rows. What pairs them is the marker, not the asset.
func crossChainTransferTx(srcAcctID, dstAcctID uuid.UUID, amount int64) *Transaction {
	return &Transaction{
		ID:   uuid.New(),
		Type: TxTypeInternalTransfer,
		Entries: []*Entry{
			makeEntry(dstAcctID, Debit, EntryTypeAssetIncrease, amount, testasset.ETHOnArbitrum, map[string]interface{}{
				"chain_id":  "arbitrum",
				MetaLegPair: "bridge",
			}),
			makeEntry(srcAcctID, Credit, EntryTypeAssetDecrease, amount, testasset.ETH, map[string]interface{}{
				"chain_id":  "base",
				MetaLegPair: "bridge",
			}),
		},
	}
}

// TestTaxLotHook_CrossChainInternalTransfer_CarriesBasisNoPnL is the accounting
// heart of the epic's bridge story. A lot bought at $100/unit on Base is
// bridged to Arbitrum while the market sits at $200/unit. The destination lot
// must inherit $100 — the cost basis, not the bridge-time price — and nothing
// may be realized.
func TestTaxLotHook_CrossChainInternalTransfer_CarriesBasisNoPnL(t *testing.T) {
	baseAcctID := uuid.New()
	arbAcctID := uuid.New()

	// Acquired on Base at $100/unit (scaled 10^8). makeEntry prices the bridge
	// entries at $200/unit, so FMV-at-transfer and carried basis are distinct
	// numbers and the assertion can tell them apart.
	sourceLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            baseAcctID,
		Asset:                testasset.ETH,
		QuantityAcquired:     big.NewInt(1000),
		QuantityRemaining:    big.NewInt(1000),
		AcquiredAt:           time.Now().Add(-24 * time.Hour),
		AutoCostBasisPerUnit: big.NewInt(100_000_000), // $100
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            time.Now(),
	}

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{sourceLot}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		baseAcctID: chainWalletAccount(baseAcctID, "base", testasset.ETH),
		arbAcctID:  chainWalletAccount(arbAcctID, "arbitrum", testasset.ETHOnArbitrum),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	if err := hook(context.Background(), crossChainTransferTx(baseAcctID, arbAcctID, 1000)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// --- The source lot is consumed as an internal transfer, not a sale ---
	if len(taxLotRepo.disposals) != 1 {
		t.Fatalf("expected 1 disposal on the source chain, got %d", len(taxLotRepo.disposals))
	}
	disposal := taxLotRepo.disposals[0]
	if disposal.DisposalType != DisposalTypeInternalTransfer {
		t.Errorf("bridging own funds must NOT be a sale: expected disposal type %s, got %s",
			DisposalTypeInternalTransfer, disposal.DisposalType)
	}
	if disposal.LotID != sourceLot.ID {
		t.Errorf("expected the Base lot to be consumed, got lot %s", disposal.LotID)
	}
	if sourceLot.QuantityRemaining.Sign() != 0 {
		t.Errorf("expected the source lot fully consumed, %s remaining", sourceLot.QuantityRemaining)
	}

	// --- The destination lot links back and inherits the basis ---
	if len(taxLotRepo.lots) != 2 {
		t.Fatalf("expected the source lot plus one new destination lot, got %d", len(taxLotRepo.lots))
	}
	destLot := taxLotRepo.lots[1]

	if destLot.AccountID != arbAcctID {
		t.Errorf("the new lot must live on the DESTINATION chain account, got %s", destLot.AccountID)
	}
	if destLot.LinkedSourceLotID == nil {
		t.Fatal("destination lot must link to the consumed source lot; without the link the bridge " +
			"looks like an unrelated acquisition and the basis chain is broken")
	}
	if *destLot.LinkedSourceLotID != sourceLot.ID {
		t.Errorf("expected link to source lot %s, got %s", sourceLot.ID, *destLot.LinkedSourceLotID)
	}
	if destLot.AutoCostBasisPerUnit.Cmp(big.NewInt(100_000_000)) != 0 {
		t.Errorf("cost basis must CARRY ACROSS the bridge ($100), not reset to bridge-time FMV ($200): got %s",
			destLot.AutoCostBasisPerUnit)
	}
	if destLot.AutoCostBasisSource != CostBasisLinkedTransfer {
		t.Errorf("expected basis source %s, got %s", CostBasisLinkedTransfer, destLot.AutoCostBasisSource)
	}
	if destLot.PriceStatus != PriceStatusResolved {
		t.Errorf("a carried basis is resolved by construction, got %s", destLot.PriceStatus)
	}
}

// TestTaxLotHook_CrossChainInternalTransfer_WeightedAverageAcrossLots: a bridge
// large enough to consume several source lots must carry their WEIGHTED AVERAGE
// basis, not the first lot's. Getting this wrong silently misstates basis on
// exactly the wallets that trade most.
func TestTaxLotHook_CrossChainInternalTransfer_WeightedAverageAcrossLots(t *testing.T) {
	baseAcctID := uuid.New()
	arbAcctID := uuid.New()

	// 600 @ $100 (older, consumed first by FIFO) + 400 @ $200
	// weighted average = (600*100 + 400*200) / 1000 = $140
	older := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            baseAcctID,
		Asset:                testasset.ETH,
		QuantityAcquired:     big.NewInt(600),
		QuantityRemaining:    big.NewInt(600),
		AcquiredAt:           time.Now().Add(-48 * time.Hour),
		AutoCostBasisPerUnit: big.NewInt(100_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            time.Now(),
	}
	newer := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            baseAcctID,
		Asset:                testasset.ETH,
		QuantityAcquired:     big.NewInt(400),
		QuantityRemaining:    big.NewInt(400),
		AcquiredAt:           time.Now().Add(-24 * time.Hour),
		AutoCostBasisPerUnit: big.NewInt(200_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            time.Now(),
	}

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{older, newer}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		baseAcctID: chainWalletAccount(baseAcctID, "base", testasset.ETH),
		arbAcctID:  chainWalletAccount(arbAcctID, "arbitrum", testasset.ETHOnArbitrum),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	if err := hook(context.Background(), crossChainTransferTx(baseAcctID, arbAcctID, 1000)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.disposals) != 2 {
		t.Fatalf("expected both source lots consumed, got %d disposals", len(taxLotRepo.disposals))
	}
	for _, d := range taxLotRepo.disposals {
		if d.DisposalType != DisposalTypeInternalTransfer {
			t.Errorf("every leg of a bridge disposal is an internal transfer, got %s", d.DisposalType)
		}
	}

	if len(taxLotRepo.lots) != 3 {
		t.Fatalf("expected two source lots plus one destination lot, got %d", len(taxLotRepo.lots))
	}
	destLot := taxLotRepo.lots[2]

	if destLot.AutoCostBasisPerUnit.Cmp(big.NewInt(140_000_000)) != 0 {
		t.Errorf("expected weighted-average basis $140 across the two consumed lots, got %s",
			destLot.AutoCostBasisPerUnit)
	}
	if destLot.LinkedSourceLotID == nil || *destLot.LinkedSourceLotID != older.ID {
		t.Errorf("expected the link to point at the FIRST consumed lot %s", older.ID)
	}
}

// TestTaxLotHook_SameChainInternalTransfer_Unchanged pins the same-chain
// behaviour so neither #32 nor #84 can be shown to have changed it. Here the
// two legs genuinely ARE the same asset — one chain, one contract — and the
// carry-over still runs off the marker, not off that coincidence. This is the
// case that rules out "pair the legs when the chains differ" as an inference:
// the chains are equal and the basis must still carry.
func TestTaxLotHook_SameChainInternalTransfer_Unchanged(t *testing.T) {
	srcAcctID := uuid.New()
	dstAcctID := uuid.New()

	sourceLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            srcAcctID,
		Asset:                testasset.ETH,
		QuantityAcquired:     big.NewInt(1000),
		QuantityRemaining:    big.NewInt(1000),
		AcquiredAt:           time.Now().Add(-24 * time.Hour),
		AutoCostBasisPerUnit: big.NewInt(100_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		PriceStatus:          PriceStatusResolved,
		CreatedAt:            time.Now(),
	}

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
			makeEntry(dstAcctID, Debit, EntryTypeAssetIncrease, 1000, testasset.ETH, map[string]interface{}{"chain_id": "base", MetaLegPair: "move"}),
			makeEntry(srcAcctID, Credit, EntryTypeAssetDecrease, 1000, testasset.ETH, map[string]interface{}{"chain_id": "base", MetaLegPair: "move"}),
		},
	}

	if err := hook(context.Background(), tx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.disposals) != 1 || taxLotRepo.disposals[0].DisposalType != DisposalTypeInternalTransfer {
		t.Fatalf("same-chain internal transfer must still dispose as an internal transfer")
	}
	destLot := taxLotRepo.lots[1]
	if destLot.LinkedSourceLotID == nil || *destLot.LinkedSourceLotID != sourceLot.ID {
		t.Error("same-chain internal transfer must still link the destination lot to the source lot")
	}
	if destLot.AutoCostBasisPerUnit.Cmp(big.NewInt(100_000_000)) != 0 {
		t.Errorf("same-chain internal transfer must still carry basis, got %s", destLot.AutoCostBasisPerUnit)
	}
}
