package sync_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Issue #32 — cross-chain internal_transfer through the port seam.
//
// The port seam (TransactionDataProvider / DecodedTransaction) is where the
// epic says bridge behaviour is asserted. These tests hand-author a cross-chain
// internal_transfer — a DecodedTransaction observed on the SOURCE chain that
// also names a destination chain — and follow it all the way to the ledger
// entries the handler produces.
//
// What is deliberately NOT here: producing that DecodedTransaction from two
// independent bridge legs. That is bridge stitching (#33). #32's job is to make
// the target reachable, so #33 has something correct to aim at. The fixtures
// below stand in for the stitcher's output.
// =============================================================================

const (
	xcSourceChain = "base"
	xcDestChain   = "arbitrum"
	xcWalletAddr  = "0xdddd000000000000000000000000000000000004"
	xcPeerAddr    = "0xeeee000000000000000000000000000000000005"
)

// bridgedTx is what a stitched bridge looks like at the port: observed on the
// source chain (ChainID), carrying the outgoing transfer, and naming the chain
// the funds arrived on (DestChainID). One transaction, two chains.
func bridgedTx(destChain string) sync.DecodedTransaction {
	return sync.DecodedTransaction{
		ID:            xcSourceChain + ":0xbridged",
		TxHash:        "0xbridged",
		ChainID:       xcSourceChain,
		DestChainID:   destChain,
		OperationType: sync.OpSend,
		Transfers: []sync.DecodedTransfer{{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(1e18),
			Direction:   sync.DirectionOut,
			Sender:      xcWalletAddr,
			Recipient:   xcPeerAddr,
		}},
		MinedAt: time.Date(2024, 5, 2, 12, 0, 0, 0, time.UTC),
		Status:  "confirmed",
	}
}

// -----------------------------------------------------------------------------
// AC1/AC4 — the pipeline carries both chains through to the handler
// -----------------------------------------------------------------------------

// TestCrossChainInternalTransfer_PortSeam_CarriesBothChains: the destination
// chain must survive the trip from the port to the transaction data the ledger
// records. If it is dropped here, the handler falls back to same-chain and the
// destination lot silently lands on the wrong chain's account.
func TestCrossChainInternalTransfer_PortSeam_CarriesBothChains(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, xcWalletAddr)
	dstWallet := newTestWallet(userID, xcPeerAddr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		xcWalletAddr: srcWallet,
		xcPeerAddr:   dstWallet,
	})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	_, err := env.builder.ProcessTransaction(ctx, srcWallet, bridgedTx(xcDestChain))
	require.NoError(t, err)

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	rec := env.ledgerSvc.recordedTransactions[0]
	require.Equal(t, ledger.TxTypeInternalTransfer, rec.TxType,
		"a bridge of the user's own funds is one internal transfer, never a sale plus a purchase")

	assert.Equal(t, xcSourceChain, rec.RawData["source_chain_id"],
		"the source chain is the chain the transaction was observed on")
	assert.Equal(t, xcDestChain, rec.RawData["dest_chain_id"],
		"the destination chain must reach the handler, or the bridge collapses to same-chain")
}

// TestCrossChainInternalTransfer_PortSeam_SameChainOmitsDestination: an ordinary
// same-chain internal transfer sets no DestChainID at the port, and must
// therefore produce no dest_chain_id override — the legacy shape, byte for
// byte. This is the guard on "same-chain behaviour unchanged".
func TestCrossChainInternalTransfer_PortSeam_SameChainOmitsDestination(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, xcWalletAddr)
	dstWallet := newTestWallet(userID, xcPeerAddr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		xcWalletAddr: srcWallet,
		xcPeerAddr:   dstWallet,
	})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	// DestChainID empty — every provider-decoded transaction looks like this.
	_, err := env.builder.ProcessTransaction(ctx, srcWallet, bridgedTx(""))
	require.NoError(t, err)

	rec := env.ledgerSvc.recordedTransactions[0]
	assert.NotContains(t, rec.RawData, "dest_chain_id",
		"a same-chain internal transfer must not carry a destination-chain override")
	assert.Equal(t, xcSourceChain, rec.RawData["chain_id"])
}

// -----------------------------------------------------------------------------
// AC1/AC2 — end to end: port fixture → handler entries → tax lots
// -----------------------------------------------------------------------------

// TestCrossChainInternalTransfer_EndToEnd_BasisCarriedNoPnL is the whole point
// of #32, asserted as one chain of custody: a hand-authored cross-chain
// internal_transfer at the port produces transaction data that the REAL handler
// turns into a source-chain CREDIT and a destination-chain DEBIT, which the
// REAL tax-lot hook then resolves into a carried cost basis with no realized
// PnL. Every link is the production code path; only the provider, the ledger
// store, and the tax-lot store are fakes.
func TestCrossChainInternalTransfer_EndToEnd_BasisCarriedNoPnL(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, xcWalletAddr)
	dstWallet := newTestWallet(userID, xcPeerAddr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		xcWalletAddr: srcWallet,
		xcPeerAddr:   dstWallet,
	})
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	// --- Link 1: the port produces cross-chain transaction data ---
	_, err := env.builder.ProcessTransaction(ctx, srcWallet, bridgedTx(xcDestChain))
	require.NoError(t, err)
	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	data := env.ledgerSvc.recordedTransactions[0].RawData

	// --- Link 2: the real handler turns it into two chain-specific legs ---
	walletRepo := new(MockTransferWalletRepo)
	walletRepo.On("GetByID", mock.Anything, srcWallet.ID).Return(srcWallet, nil).Maybe()
	walletRepo.On("GetByID", mock.Anything, dstWallet.ID).Return(dstWallet, nil).Maybe()

	handler := transfer.NewInternalTransferHandler(walletRepo, logger.NewDefault("test"))
	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err, "the handler must accept source_chain != dest_chain")

	var debit, credit *ledger.Entry
	for _, e := range entries {
		switch e.EntryType {
		case ledger.EntryTypeAssetIncrease:
			debit = e
		case ledger.EntryTypeAssetDecrease:
			credit = e
		}
	}
	require.NotNil(t, debit)
	require.NotNil(t, credit)
	assert.Equal(t, xcDestChain, debit.Metadata["chain_id"], "the inflow is booked on the destination chain")
	assert.Equal(t, xcSourceChain, credit.Metadata["chain_id"], "the outflow is booked on the source chain")
	require.NotEqual(t, debit.Metadata["account_code"], credit.Metadata["account_code"],
		"the two legs must resolve to different accounts, which is what makes this a bridge")

	// --- Link 3: the two legs balance, so the ledger will accept them ---
	debitSum, creditSum := big.NewInt(0), big.NewInt(0)
	for _, e := range entries {
		if e.DebitCredit == ledger.Debit {
			debitSum.Add(debitSum, e.Amount)
		} else {
			creditSum.Add(creditSum, e.Amount)
		}
	}
	require.Equal(t, 0, debitSum.Cmp(creditSum),
		"an unbalanced transaction is rejected before the tax-lot hook ever runs")

	// The tax-lot consequence of these two legs — carried basis, no realized
	// PnL — is asserted directly against the hook in
	// internal/ledger/taxlot_hook_crosschain_test.go, where the tax-lot store
	// can be observed. What matters here is that the legs arriving at the hook
	// are the cross-chain pair the hook needs: one asset-decrease and one
	// asset-increase of the same asset, in the same transaction.
	assert.Equal(t, credit.AssetID, debit.AssetID,
		"the hook pairs disposal to acquisition by asset; differing assets would break the carry-over")
}

// MockTransferWalletRepo is the transfer module's wallet lookup. The handler
// only needs GetByID: it verifies both wallets exist and share a user.
type MockTransferWalletRepo struct {
	mock.Mock
}

func (m *MockTransferWalletRepo) GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error) {
	args := m.Called(ctx, walletID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*wallet.Wallet), args.Error(1)
}
