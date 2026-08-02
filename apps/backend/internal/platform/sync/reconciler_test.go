package sync_test

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pkgsync "github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

func newTestReconciler(
	rawTxRepo pkgsync.RawTransactionRepository,
	posProvider pkgsync.PositionDataProvider,
	walletRepo pkgsync.WalletRepository,
	assetRepo pkgsync.SyncAssetRepository,
) *pkgsync.Reconciler {
	log := logger.New("test", os.Stdout)
	return pkgsync.NewReconciler(rawTxRepo, posProvider, walletRepo, assetRepo, log)
}

// buildSendRaw creates a raw transaction with a single outbound transfer of the
// given asset/decimals/amount, used to seed calculated net flows.
func buildSendRaw(walletID uuid.UUID, symbol string, decimals int, amount *big.Int, minedAt time.Time) *pkgsync.RawTransaction {
	dt := pkgsync.DecodedTransaction{
		ID:            "tx-send-1",
		TxHash:        "0xaaa",
		ChainID:       "ethereum",
		OperationType: pkgsync.OpSend,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: symbol,
			Decimals:    decimals,
			Amount:      amount,
			Direction:   pkgsync.DirectionOut,
		}},
		MinedAt: minedAt,
		Status:  "confirmed",
	}
	return &pkgsync.RawTransaction{
		ID: uuid.New(), WalletID: walletID, ExternalID: "tx-send-1", TxHash: "0xaaa",
		ChainID: "ethereum", OperationType: "send", MinedAt: minedAt, Status: "confirmed",
		RawJSON: marshalDecodedTx(dt), ProcessingStatus: pkgsync.ProcessingStatusPending,
	}
}

// buildRecvRaw creates a raw transaction with a single inbound transfer of the
// given asset/decimals/amount, used to seed calculated net flows.
func buildRecvRaw(walletID uuid.UUID, symbol string, decimals int, amount *big.Int, minedAt time.Time) *pkgsync.RawTransaction {
	dt := pkgsync.DecodedTransaction{
		ID:            "tx-recv-1",
		TxHash:        "0xbbb",
		ChainID:       "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: symbol,
			Decimals:    decimals,
			Amount:      amount,
			Direction:   pkgsync.DirectionIn,
		}},
		MinedAt: minedAt,
		Status:  "confirmed",
	}
	return &pkgsync.RawTransaction{
		ID: uuid.New(), WalletID: walletID, ExternalID: "tx-recv-1", TxHash: "0xbbb",
		ChainID: "ethereum", OperationType: "receive", MinedAt: minedAt, Status: "confirmed",
		RawJSON: marshalDecodedTx(dt), ProcessingStatus: pkgsync.ProcessingStatusPending,
	}
}

// TestReconcile_DeltaBeyondDust_FlagsChainAndWritesNothing is the port-level
// statement of issue #53: a position that disagrees with the transaction history
// produces a FLAG and nothing else.
//
// Reconciliation used to answer a positive delta by fabricating a
// `genesis_balance` — income out of nowhere, at a cost basis of zero that no
// backfill would ever revisit. That erased the very discrepancy the delta had
// just detected. The delta is now a verdict: it reaches the chain's error field
// and never reaches the ledger.
//
// The assertion that carries the ticket is the negative one: NO raw transaction
// is written. A raw is the only route from the reconciler into the ledger, so
// "no raw upserted" is precisely "no entries and no lots" — the reconciler
// cannot produce either without first producing a raw for the Processor to book.
func TestReconcile_DeltaBeyondDust_FlagsChainAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, "0x1111111111111111111111111111111111111111")
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	walletRepo := new(MockWalletRepository)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
	walletRepo.On("SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything).Return(nil)
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
		{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
	}, nil)

	// Sent 1 USDC out (net flow -1); on-chain shows 2 USDC. Delta = 2 - (-1) = +3.
	// This is exactly the shape that used to synthesize a 3 USDC genesis.
	sendRaw := buildSendRaw(w.ID, "USDC", 6, big.NewInt(1_000_000), t1)
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{sendRaw}, nil)
	posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(2_000_000)},
	}, nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err, "a per-chain discrepancy is isolated, not a wallet-wide abort")
	require.Equal(t, 1, count, "the discrepancy is counted as flagged, not as repaired")

	// The flag lands on the chain of the offending position.
	walletRepo.AssertCalled(t, "SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything)

	// Nothing is written. No raw means no ledger transaction, so no entries and
	// no tax lots can exist for this delta.
	rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)
}

// TestReconcile_DeltaSignSymmetry_SameHandling is the second port-level
// statement of issue #53: the SIGN of the delta does not change what happens.
//
// The old asymmetry was not a judgement about the two directions — it was an
// artifact of capability. A positive delta could be "fixed" by inventing income,
// a negative one could not, so only the negative one was flagged. With the fix
// removed there is nothing left to justify treating them differently: both mean
// the position and the history disagree.
//
// Both halves use the SAME magnitude (3 USDC) so that the only difference
// between them is the sign.
func TestReconcile_DeltaSignSymmetry_SameHandling(t *testing.T) {
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	const magnitude = 3_000_000 // 3 USDC, identical in both directions

	// runOneSided reconciles a wallet whose only flow is `raw` against a single
	// on-chain position of `onChain`, and reports what the reconciler did.
	runOneSided := func(t *testing.T, w *wallet.Wallet, raw *pkgsync.RawTransaction, onChain *big.Int) (count int, flagged bool) {
		t.Helper()
		ctx := context.Background()

		walletRepo := new(MockWalletRepository)
		posProvider := new(MockPositionDataProvider)
		rawTxRepo := new(MockRawTransactionRepository)

		walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
		walletRepo.On("SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything).Return(nil).Maybe()
		walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
			{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
		}, nil)
		rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{raw}, nil)
		posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
			{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: onChain},
		}, nil)

		r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
		count, err := r.Reconcile(ctx, w)
		require.NoError(t, err)

		// Neither sign may write anything.
		rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)

		for _, call := range walletRepo.Calls {
			if call.Method == "SetChainSyncError" {
				flagged = true
			}
		}
		return count, flagged
	}

	userID := uuid.New()

	// Positive: received 1 USDC, on-chain shows 4 → delta = 4 - 1 = +3.
	wPos := newTestWallet(userID, "0x1111111111111111111111111111111111111111")
	posCount, posFlagged := runOneSided(t, wPos,
		buildRecvRaw(wPos.ID, "USDC", 6, big.NewInt(1_000_000), t1),
		big.NewInt(1_000_000+magnitude))

	// Negative: received 4 USDC, on-chain shows 1 → delta = 1 - 4 = -3.
	wNeg := newTestWallet(userID, "0x2222222222222222222222222222222222222222")
	negCount, negFlagged := runOneSided(t, wNeg,
		buildRecvRaw(wNeg.ID, "USDC", 6, big.NewInt(1_000_000+magnitude), t1),
		big.NewInt(1_000_000))

	assert.True(t, posFlagged, "a positive delta beyond dust must flag the chain")
	assert.True(t, negFlagged, "a negative delta beyond dust must flag the chain")

	// Pin the absolute value first, so the equality below cannot pass vacuously
	// by both sides being zero.
	assert.Equal(t, 1, posCount, "the positive delta is flagged exactly once")
	assert.Equal(t, posCount, negCount,
		"the sign of the delta must not change the outcome: same magnitude, same handling")
}

// TestReconcile_DeltaWithinDust_NoFlag verifies the dust tolerance still holds,
// and — per issue #53 — that it is applied to the ABSOLUTE value, so rounding
// noise is tolerated in both directions rather than only below the line.
func TestReconcile_DeltaWithinDust_NoFlag(t *testing.T) {
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		flow    *big.Int // net inbound flow from the transaction history
		onChain *big.Int
	}{
		// delta = 995 - 1000 = -5, within the ±10 dust band.
		{name: "negative dust", flow: big.NewInt(1000), onChain: big.NewInt(995)},
		// delta = 1005 - 1000 = +5, the mirror image.
		{name: "positive dust", flow: big.NewInt(1000), onChain: big.NewInt(1005)},
		// delta = 1010 - 1000 = +10, exactly at the tolerance — still dust.
		{name: "positive dust at boundary", flow: big.NewInt(1000), onChain: big.NewInt(1010)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			userID := uuid.New()
			w := newTestWallet(userID, "0x1111111111111111111111111111111111111111")

			walletRepo := new(MockWalletRepository)
			posProvider := new(MockPositionDataProvider)
			rawTxRepo := new(MockRawTransactionRepository)

			walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
			walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
				{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
			}, nil)
			rawTxRepo.On("GetAllByWallet", ctx, w.ID).
				Return([]*pkgsync.RawTransaction{buildRecvRaw(w.ID, "USDC", 6, tc.flow, t1)}, nil)
			posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
				{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: tc.onChain},
			}, nil)

			r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
			count, err := r.Reconcile(ctx, w)

			require.NoError(t, err)
			require.Equal(t, 0, count, "dust is rounding noise, not a discrepancy")
			walletRepo.AssertNotCalled(t, "SetChainSyncError", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)
		})
	}
}

// TestReconcile_FlaggedChainDoesNotStopOtherChains verifies that flagging a chain
// is an indicator, not an abort (issue #28 preserved under #53): a discrepancy on
// one chain must not prevent the wallet's other chains from being reconciled.
func TestReconcile_FlaggedChainDoesNotStopOtherChains(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, "0x1111111111111111111111111111111111111111")
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	walletRepo := new(MockWalletRepository)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
	walletRepo.On("SetChainSyncError", ctx, w.ID, mock.Anything, mock.Anything).Return(nil)
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
		{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
		{WalletID: w.ID, Chain: "base", SyncStatus: wallet.SyncStatusPending},
	}, nil)

	// Only an ethereum flow exists. Both chains report an unexplained position, so
	// both must be flagged — the first must not short-circuit the second.
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).
		Return([]*pkgsync.RawTransaction{buildRecvRaw(w.ID, "USDC", 6, big.NewInt(1_000_000), t1)}, nil)
	posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(9_000_000)},
	}, nil)
	posProvider.On("GetPositions", ctx, w.Address, "base").Return([]pkgsync.OnChainPosition{
		{ChainID: "base", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(5_000_000)},
	}, nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err)
	require.Equal(t, 2, count, "both chains are examined; a flag on one does not stop the other")
	walletRepo.AssertCalled(t, "SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything)
	walletRepo.AssertCalled(t, "SetChainSyncError", ctx, w.ID, "base", mock.Anything)
	posProvider.AssertCalled(t, "GetPositions", ctx, w.Address, "base")
	rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)
}

// TestReconcile_DecimalsMismatch_MarksDegraded verifies MT-SYNC-04: when the
// calculated flow's decimals disagree with the on-chain position's decimals, the
// two numbers are not on the same scale, so the delta between them is
// meaningless. The reconciler flags the chain and skips the position rather than
// reporting a delta it cannot compute. Per issue #28 this degrades only the
// offending CHAIN (SetChainSyncError) and the reconcile continues.
func TestReconcile_DecimalsMismatch_MarksDegraded(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, "0x1111111111111111111111111111111111111111")
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	walletRepo := new(MockWalletRepository)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
	walletRepo.On("SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything).Return(nil)
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
		{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
	}, nil)

	// Flow decimals recorded as 18 (from the transfer), but on-chain position is 6.
	sendRaw := buildSendRaw(w.ID, "USDC", 18, big.NewInt(1_000_000), t1)
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{sendRaw}, nil)
	posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(2_000_000)},
	}, nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err, "a per-chain discrepancy is isolated, not a wallet-wide abort")
	require.Equal(t, 1, count, "an uncomparable position is itself a flagged discrepancy")
	walletRepo.AssertCalled(t, "SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything)
	rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)
}
