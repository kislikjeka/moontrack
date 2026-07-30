package sync_test

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
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

// TestReconcile_PositiveDelta_SynthesizesGenesis verifies the happy path is
// unchanged: on-chain > calculated produces exactly one genesis raw transaction.
func TestReconcile_PositiveDelta_SynthesizesGenesis(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, "0x1111111111111111111111111111111111111111")
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	walletRepo := new(MockWalletRepository)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, w.ID).Return(nil)
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
		{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
	}, nil)

	// Sent 1 USDC out; on-chain shows 2 USDC → delta = 2 - (-1) = 3 USDC genesis.
	sendRaw := buildSendRaw(w.ID, "USDC", 6, big.NewInt(1_000_000), t1)
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{sendRaw}, nil)
	posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(2_000_000)},
	}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, w.ID).Return(&t1, nil)
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err)
	require.Equal(t, 1, count)
	rawTxRepo.AssertCalled(t, "UpsertRawTransaction", ctx, mock.Anything)
	walletRepo.AssertNotCalled(t, "SetSyncError", mock.Anything, mock.Anything, mock.Anything)
}

// TestReconcile_NegativeDeltaBeyondDust_MarksDegraded verifies MT-SYNC-03: a
// negative delta beyond the dust tolerance is surfaced rather than silently
// skipped, and no genesis is synthesized. Per issue #28 the discrepancy degrades
// only the offending CHAIN (SetChainSyncError) and the reconcile continues rather
// than aborting the whole wallet — the wallet-level error is a rollup concern.
func TestReconcile_NegativeDeltaBeyondDust_MarksDegraded(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, "0x1111111111111111111111111111111111111111")
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	walletRepo := new(MockWalletRepository)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
	walletRepo.On("SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything).Return(nil)
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, w.ID).Return(nil)
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
		{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
	}, nil)

	// Received 5 USDC (net inflow +5), but on-chain shows only 2 USDC.
	// Delta = 2 - 5 = -3 USDC, far beyond dust → degraded.
	recvRaw := &pkgsync.RawTransaction{
		ID: uuid.New(), WalletID: w.ID, ExternalID: "tx-recv-1", TxHash: "0xbbb",
		ChainID: "ethereum", OperationType: "receive", MinedAt: t1, Status: "confirmed",
		RawJSON: marshalDecodedTx(pkgsync.DecodedTransaction{
			ID: "tx-recv-1", TxHash: "0xbbb", ChainID: "ethereum", OperationType: pkgsync.OpReceive,
			Transfers: []pkgsync.DecodedTransfer{{
				AssetSymbol: "USDC", Decimals: 6, Amount: big.NewInt(5_000_000), Direction: pkgsync.DirectionIn,
			}},
			MinedAt: t1, Status: "confirmed",
		}),
		ProcessingStatus: pkgsync.ProcessingStatusPending,
	}
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{recvRaw}, nil)
	posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(2_000_000)},
	}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, w.ID).Return(&t1, nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err, "a per-chain discrepancy is isolated, not a wallet-wide abort")
	require.Equal(t, 0, count)
	walletRepo.AssertCalled(t, "SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything)
	rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)
}

// TestReconcile_NegativeDeltaWithinDust_Skipped verifies that a tiny negative
// delta (rounding noise) is still tolerated: no genesis, no degradation.
func TestReconcile_NegativeDeltaWithinDust_Skipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, "0x1111111111111111111111111111111111111111")
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	walletRepo := new(MockWalletRepository)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, w.ID).Return(nil)
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
		{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
	}, nil)

	// Received 1000 base units; on-chain shows 995 → delta = -5, within dust (<=10).
	recvRaw := &pkgsync.RawTransaction{
		ID: uuid.New(), WalletID: w.ID, ExternalID: "tx-recv-1", TxHash: "0xbbb",
		ChainID: "ethereum", OperationType: "receive", MinedAt: t1, Status: "confirmed",
		RawJSON: marshalDecodedTx(pkgsync.DecodedTransaction{
			ID: "tx-recv-1", TxHash: "0xbbb", ChainID: "ethereum", OperationType: pkgsync.OpReceive,
			Transfers: []pkgsync.DecodedTransfer{{
				AssetSymbol: "USDC", Decimals: 6, Amount: big.NewInt(1000), Direction: pkgsync.DirectionIn,
			}},
			MinedAt: t1, Status: "confirmed",
		}),
		ProcessingStatus: pkgsync.ProcessingStatusPending,
	}
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{recvRaw}, nil)
	posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(995)},
	}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, w.ID).Return(&t1, nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err)
	require.Equal(t, 0, count)
	walletRepo.AssertNotCalled(t, "SetSyncError", mock.Anything, mock.Anything, mock.Anything)
	rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)
}

// TestReconcile_DecimalsMismatch_MarksDegraded verifies MT-SYNC-04: when the
// calculated flow's decimals disagree with the on-chain position's decimals, the
// reconciler surfaces it and does NOT synthesize a genesis. Per issue #28 this
// degrades only the offending CHAIN (SetChainSyncError) and the reconcile
// continues rather than aborting the whole wallet.
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
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, w.ID).Return(nil)
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return([]wallet.WalletChainSync{
		{WalletID: w.ID, Chain: "ethereum", SyncStatus: wallet.SyncStatusPending},
	}, nil)

	// Flow decimals recorded as 18 (from the transfer), but on-chain position is 6.
	sendRaw := buildSendRaw(w.ID, "USDC", 18, big.NewInt(1_000_000), t1)
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{sendRaw}, nil)
	posProvider.On("GetPositions", ctx, w.Address, "ethereum").Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(2_000_000)},
	}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, w.ID).Return(&t1, nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err, "a per-chain discrepancy is isolated, not a wallet-wide abort")
	require.Equal(t, 0, count)
	walletRepo.AssertCalled(t, "SetChainSyncError", ctx, w.ID, "ethereum", mock.Anything)
	rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)
}
