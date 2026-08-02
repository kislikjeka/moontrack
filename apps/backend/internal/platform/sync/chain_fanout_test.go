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

	pkgsync "github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// receiveTxOn builds a minimal inbound DecodedTransaction on the given chain.
func receiveTxOn(chain, id string) pkgsync.DecodedTransaction {
	return pkgsync.DecodedTransaction{
		ID: id, TxHash: "0x" + id, ChainID: chain,
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(1e18),
			Direction: pkgsync.DirectionIn,
		}},
		MinedAt: time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
		Status:  "confirmed",
	}
}

// TestCollector_FansOutOverEnabledChains is the #27 port-seam behavior: the
// collector iterates the wallet's chain-set rows (wallet_chain_sync), invoking
// the chain-aware provider exactly once per enabled chain, and stores a raw per
// chain. A wallet with 3 enabled chains produces data across all 3.
func TestCollector_FansOutOverEnabledChains(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	w := &wallet.Wallet{ID: walletID, Address: walletAddr}

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	// The wallet's chain set: the three Enabled chains.
	chains := []string{"ethereum", "base", "arbitrum"}
	rows := make([]wallet.WalletChainSync, 0, len(chains))
	for _, c := range chains {
		rows = append(rows, wallet.WalletChainSync{WalletID: walletID, Chain: c, SyncStatus: wallet.SyncStatusPending})
	}
	walletRepo.On("GetChainSyncRows", ctx, walletID).Return(rows, nil)

	// One tx per chain; the provider returns it only for the matching chain.
	for _, c := range chains {
		provider.On("GetTransactions", ctx, walletAddr, c, mock.Anything).
			Return([]pkgsync.DecodedTransaction{receiveTxOn(c, "tx-"+c)}, nil)
	}

	// Each chain advances its own collect cursor; the wallet-level cursor is the max.
	seenChainCursors := map[string]bool{}
	walletRepo.On("SetChainCollectCursor", ctx, walletID, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { seenChainCursors[args.Get(2).(string)] = true }).
		Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	// Capture the chains of stored raws.
	storedChains := map[string]bool{}
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			raw := args.Get(1).(*pkgsync.RawTransaction)
			storedChains[raw.ChainID] = true
		}).Return(nil)

	collector := newTestCollector(provider, rawTxRepo, walletRepo, nil)
	count, err := collector.CollectAll(ctx, w)

	require.NoError(t, err)
	assert.Equal(t, 3, count, "one tx stored per enabled chain")

	// Provider invoked exactly once per enabled chain.
	for _, c := range chains {
		provider.AssertCalled(t, "GetTransactions", ctx, walletAddr, c, mock.Anything)
		assert.True(t, storedChains[c], "raw stored for chain %s", c)
		assert.True(t, seenChainCursors[c], "collect cursor advanced for chain %s", c)
	}
	provider.AssertNumberOfCalls(t, "GetTransactions", 3)
}

// TestReconciler_FansOutOverEnabledChains verifies Phase 2 runs per enabled
// chain: the reconciler invokes the chain-aware position provider once per chain
// in the wallet's chain set, and an unexplained balance on each chain flags that
// chain (issue #53 — the flag lands on the chain the discrepancy is on, and a
// flag on one chain does not stop the others).
func TestReconciler_FansOutOverEnabledChains(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, "0x2222222222222222222222222222222222222222")

	walletRepo := new(MockWalletRepository)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)

	chains := []string{"ethereum", "base", "arbitrum"}
	rows := make([]wallet.WalletChainSync, 0, len(chains))
	for _, c := range chains {
		rows = append(rows, wallet.WalletChainSync{WalletID: w.ID, Chain: c, SyncStatus: wallet.SyncStatusPending})
	}
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return(rows, nil)

	// No collected raws → no calculated flow; each chain shows an on-chain balance
	// nothing explains → one flagged discrepancy per chain.
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{}, nil)

	for _, c := range chains {
		posProvider.On("GetPositions", ctx, w.Address, c).Return([]pkgsync.OnChainPosition{
			{ChainID: c, AssetSymbol: "ETH", Decimals: 18, Quantity: big.NewInt(1e18)},
		}, nil)
	}

	flaggedChains := map[string]bool{}
	walletRepo.On("SetChainSyncError", ctx, w.ID, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			flaggedChains[args.Get(2).(string)] = true
		}).Return(nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err)
	assert.Equal(t, 3, count, "one discrepancy flagged per enabled chain")
	for _, c := range chains {
		posProvider.AssertCalled(t, "GetPositions", ctx, w.Address, c)
		assert.True(t, flaggedChains[c], "chain %s flagged", c)
	}
	rawTxRepo.AssertNotCalled(t, "UpsertRawTransaction", mock.Anything, mock.Anything)
}
