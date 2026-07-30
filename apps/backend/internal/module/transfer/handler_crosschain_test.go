package transfer_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// =============================================================================
// Issue #32 — Cross-chain internal_transfer.
//
// A bridge of the user's own funds (Base → Arbitrum) is ONE economically
// neutral movement, so it is recorded as ONE internal_transfer carrying a
// source-chain CREDIT and a destination-chain DEBIT. That single transaction is
// what lets the existing TaxLotHook carry-over path do its job: the disposal on
// the source chain and the acquisition on the destination chain live in the
// same ledger transaction, so the destination lot links back to the consumed
// source lot and inherits its cost basis instead of being re-priced at FMV.
//
// The handler's contract for that is narrow: each leg's account code and
// chain_id metadata must name ITS OWN chain. The account resolver already reads
// chain_id per entry, so once the legs stop sharing one chain the resolver
// resolves two different accounts with no change of its own.
//
// Same-chain internal transfers keep the exact prior shape: source_chain and
// dest_chain default to chain_id when absent, which is what every already-
// written raw_transaction looks like.
// =============================================================================

const (
	xcSourceChain = "base"
	xcDestChain   = "arbitrum"
)

// xcWalletRepo wires two wallets of the same user, which is what an internal
// transfer (cross-chain or not) requires to validate.
func xcWalletRepo(userID, srcID, dstID uuid.UUID) *MockWalletRepository {
	repo := new(MockWalletRepository)
	repo.On("GetByID", mock.Anything, srcID).Return(&wallet.Wallet{
		ID:      srcID,
		UserID:  userID,
		Address: "0x1111111111111111111111111111111111111111",
	}, nil).Maybe()
	repo.On("GetByID", mock.Anything, dstID).Return(&wallet.Wallet{
		ID:      dstID,
		UserID:  userID,
		Address: "0x2222222222222222222222222222222222222222",
	}, nil).Maybe()
	return repo
}

// xcData is a bridge leg pair: 1 ETH leaves the source wallet on `srcChain` and
// arrives at the dest wallet on `dstChain`. Passing "" for either chain omits
// the field entirely, which is how a legacy same-chain raw looks.
func xcData(srcID, dstID uuid.UUID, srcChain, dstChain string) map[string]interface{} {
	data := map[string]interface{}{
		"source_wallet_id": srcID.String(),
		"dest_wallet_id":   dstID.String(),
		"asset_id":         "ETH",
		"decimals":         18,
		"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
		"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),        // $2000
		"chain_id":         xcSourceChain,
		"tx_hash":          "0xbridge",
		"block_number":     int64(555),
		"occurred_at":      time.Now().Add(-time.Hour).Format(time.RFC3339),
		"unique_id":        "base:0xbridge",
	}
	if srcChain != "" {
		data["source_chain_id"] = srcChain
	}
	if dstChain != "" {
		data["dest_chain_id"] = dstChain
	}
	return data
}

// legs splits generated entries into the destination-side DEBIT and the
// source-side CREDIT of the transferred asset, ignoring gas entries.
func legs(t *testing.T, entries []*ledger.Entry) (debit, credit *ledger.Entry) {
	t.Helper()
	for _, e := range entries {
		if e.EntryType == ledger.EntryTypeAssetIncrease {
			debit = e
		}
		if e.EntryType == ledger.EntryTypeAssetDecrease && e.AssetID == "ETH" {
			if md, ok := e.Metadata["entry_type"].(string); ok && md == "gas_payment" {
				continue
			}
			credit = e
		}
	}
	require.NotNil(t, debit, "expected an asset-increase (destination) entry")
	require.NotNil(t, credit, "expected an asset-decrease (source) entry")
	return debit, credit
}

// -----------------------------------------------------------------------------
// AC1 — handler accepts source_chain ≠ dest_chain
// -----------------------------------------------------------------------------

// TestInternalTransfer_CrossChain_LegsCarryOwnChains is the core of #32: one
// transaction, two chains. The source CREDIT must be booked against the source
// chain's account and the destination DEBIT against the destination chain's,
// because those are two genuinely different accounts — the account code embeds
// the chain, and a wallet's ETH on Base is not its ETH on Arbitrum.
func TestInternalTransfer_CrossChain_LegsCarryOwnChains(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	entries, err := handler.Handle(ctx, xcData(srcID, dstID, xcSourceChain, xcDestChain))
	require.NoError(t, err, "a cross-chain internal transfer must be accepted, not rejected as same-chain")

	debit, credit := legs(t, entries)

	assert.Equal(t, xcDestChain, debit.Metadata["chain_id"],
		"the destination leg must be stamped with the DESTINATION chain — the account resolver reads chain_id per entry")
	assert.Equal(t, xcSourceChain, credit.Metadata["chain_id"],
		"the source leg must keep the SOURCE chain")

	assert.Equal(t, fmt.Sprintf("wallet.%s.%s.ETH", dstID, xcDestChain), debit.Metadata["account_code"],
		"destination account code must name the destination chain, or the credit lands on the wrong balance")
	assert.Equal(t, fmt.Sprintf("wallet.%s.%s.ETH", srcID, xcSourceChain), credit.Metadata["account_code"],
		"source account code must name the source chain")

	assert.NotEqual(t, debit.Metadata["account_code"], credit.Metadata["account_code"],
		"cross-chain legs must resolve to two DIFFERENT accounts")
}

// TestInternalTransfer_CrossChain_Balances: crossing a chain boundary changes
// nothing about double-entry. The transaction must still balance exactly, or
// the ledger rejects it before the tax-lot hook ever runs.
func TestInternalTransfer_CrossChain_Balances(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	data := xcData(srcID, dstID, xcSourceChain, xcDestChain)
	data["gas_amount"] = money.NewBigIntFromInt64(21000000000000).String()
	data["gas_usd_rate"] = money.NewBigIntFromInt64(200000000000).String()
	data["gas_decimals"] = 18
	data["native_asset_id"] = "ETH"

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4, "asset pair + gas pair")

	debitSum, creditSum := big.NewInt(0), big.NewInt(0)
	for _, e := range entries {
		if e.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, e.Amount)
		} else {
			creditSum.Add(creditSum, e.Amount)
		}
	}
	assert.Equal(t, 0, debitSum.Cmp(creditSum),
		"cross-chain internal transfer must balance: debits=%s credits=%s", debitSum, creditSum)
}

// TestInternalTransfer_CrossChain_GasStaysOnSourceChain: gas for a bridge is
// paid in the SOURCE chain's native token, on the source chain. Booking it
// against the destination chain would both invent a native-token balance the
// user never had there and mis-attribute the fee.
func TestInternalTransfer_CrossChain_GasStaysOnSourceChain(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	data := xcData(srcID, dstID, xcSourceChain, xcDestChain)
	data["gas_amount"] = money.NewBigIntFromInt64(21000000000000).String()
	data["gas_usd_rate"] = money.NewBigIntFromInt64(200000000000).String()
	data["gas_decimals"] = 18
	data["native_asset_id"] = "ETH"

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)

	var gasFee, gasPayment *ledger.Entry
	for _, e := range entries {
		if e.EntryType == ledger.EntryTypeGasFee {
			gasFee = e
		}
		if md, ok := e.Metadata["entry_type"].(string); ok && md == "gas_payment" {
			gasPayment = e
		}
	}
	require.NotNil(t, gasFee)
	require.NotNil(t, gasPayment)

	assert.Equal(t, xcSourceChain, gasFee.Metadata["chain_id"],
		"gas is burned on the source chain")
	assert.Equal(t, fmt.Sprintf("gas.%s.ETH", xcSourceChain), gasFee.Metadata["account_code"])
	assert.Equal(t, xcSourceChain, gasPayment.Metadata["chain_id"],
		"gas is paid out of the source wallet's SOURCE-chain native balance")
	assert.Equal(t, fmt.Sprintf("wallet.%s.%s.ETH", srcID, xcSourceChain), gasPayment.Metadata["account_code"])
}

// -----------------------------------------------------------------------------
// AC3 — same-chain behaviour unchanged
// -----------------------------------------------------------------------------

// TestInternalTransfer_SameChain_Unchanged: every internal_transfer written
// before #32 carries only chain_id — no source_chain_id/dest_chain_id. Those
// raws must produce byte-identical entries to before, so replaying history
// after this change cannot shift a single account code.
func TestInternalTransfer_SameChain_Unchanged(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	// No source_chain_id / dest_chain_id at all — the legacy shape.
	entries, err := handler.Handle(ctx, xcData(srcID, dstID, "", ""))
	require.NoError(t, err)
	require.Len(t, entries, 2)

	debit, credit := legs(t, entries)

	assert.Equal(t, xcSourceChain, debit.Metadata["chain_id"],
		"absent dest_chain_id must fall back to chain_id — same-chain is still the default")
	assert.Equal(t, xcSourceChain, credit.Metadata["chain_id"])
	assert.Equal(t, fmt.Sprintf("wallet.%s.%s.ETH", dstID, xcSourceChain), debit.Metadata["account_code"])
	assert.Equal(t, fmt.Sprintf("wallet.%s.%s.ETH", srcID, xcSourceChain), credit.Metadata["account_code"])
}

// TestInternalTransfer_SameChain_ExplicitEqualChains: a same-chain transfer that
// spells both chains out explicitly is the same thing as one that omits them.
func TestInternalTransfer_SameChain_ExplicitEqualChains(t *testing.T) {
	ctx := context.Background()
	userID, srcID, dstID := uuid.New(), uuid.New(), uuid.New()

	handler := transfer.NewInternalTransferHandler(xcWalletRepo(userID, srcID, dstID), logger.NewDefault("test"))

	entries, err := handler.Handle(ctx, xcData(srcID, dstID, xcSourceChain, xcSourceChain))
	require.NoError(t, err)

	debit, credit := legs(t, entries)
	assert.Equal(t, xcSourceChain, debit.Metadata["chain_id"])
	assert.Equal(t, xcSourceChain, credit.Metadata["chain_id"])
}

// TestInternalTransfer_CrossChain_SameWalletDifferentChains: bridging between
// two chains of the SAME wallet address is a real and common case (one wallet,
// funds moved Base → Arbitrum). It is not the "same wallet transfer" the
// validator rejects, because the two legs are different accounts.
func TestInternalTransfer_CrossChain_SameWalletDifferentChains(t *testing.T) {
	ctx := context.Background()
	userID, walletID := uuid.New(), uuid.New()

	repo := new(MockWalletRepository)
	repo.On("GetByID", mock.Anything, walletID).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		Address: "0x1111111111111111111111111111111111111111",
	}, nil).Maybe()

	handler := transfer.NewInternalTransferHandler(repo, logger.NewDefault("test"))

	entries, err := handler.Handle(ctx, xcData(walletID, walletID, xcSourceChain, xcDestChain))
	require.NoError(t, err,
		"one wallet bridging its own funds across chains is the canonical bridge case and must be accepted")

	debit, credit := legs(t, entries)
	assert.NotEqual(t, debit.Metadata["account_code"], credit.Metadata["account_code"],
		"same wallet on two chains is two accounts")
	assert.Equal(t, xcDestChain, debit.Metadata["chain_id"])
	assert.Equal(t, xcSourceChain, credit.Metadata["chain_id"])
}

// TestInternalTransfer_SameWalletSameChain_StillRejected: the guard that
// #32 must NOT weaken. A transfer from a wallet to itself on ONE chain is a
// no-op that would fabricate a disposal and a re-acquisition of the same lot.
func TestInternalTransfer_SameWalletSameChain_StillRejected(t *testing.T) {
	ctx := context.Background()
	userID, walletID := uuid.New(), uuid.New()

	repo := new(MockWalletRepository)
	repo.On("GetByID", mock.Anything, walletID).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		Address: "0x1111111111111111111111111111111111111111",
	}, nil).Maybe()

	handler := transfer.NewInternalTransferHandler(repo, logger.NewDefault("test"))

	_, err := handler.Handle(ctx, xcData(walletID, walletID, xcSourceChain, xcSourceChain))
	require.ErrorIs(t, err, transfer.ErrSameWalletTransfer,
		"same wallet AND same chain is still a no-op that must be rejected")

	// And the legacy shape (no explicit chains) must reject identically.
	_, err = handler.Handle(ctx, xcData(walletID, walletID, "", ""))
	require.ErrorIs(t, err, transfer.ErrSameWalletTransfer)
}
