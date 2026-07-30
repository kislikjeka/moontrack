package sync_test

import (
	"context"
	"errors"
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

// chainRow builds a WalletChainSync row with an optional collect cursor.
func chainRow(walletID uuid.UUID, chain string, cursor *time.Time) wallet.WalletChainSync {
	return wallet.WalletChainSync{
		WalletID:        walletID,
		Chain:           chain,
		SyncStatus:      wallet.SyncStatusPending,
		CollectCursorAt: cursor,
	}
}

// TestCollector_OneChainErrors_OthersAdvance is the core #28 failure-isolation
// behavior at the port seam: a provider that errors on one chain must not freeze
// or corrupt the others. eth and arbitrum collect and advance their own cursors;
// base errors and is isolated — its row is marked error, its cursor is NOT
// advanced, and the collector returns no hard error (the sibling chains' work
// stands).
func TestCollector_OneChainErrors_OthersAdvance(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	addr := "0x1111111111111111111111111111111111111111"
	w := &wallet.Wallet{ID: walletID, Address: addr}

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	rows := []wallet.WalletChainSync{
		chainRow(walletID, "arbitrum", nil),
		chainRow(walletID, "base", nil),
		chainRow(walletID, "ethereum", nil),
	}
	walletRepo.On("GetChainSyncRows", ctx, walletID).Return(rows, nil)

	// eth + arbitrum return a tx; base errors.
	provider.On("GetTransactions", ctx, addr, "ethereum", mock.Anything).
		Return([]pkgsync.DecodedTransaction{receiveTxOn("ethereum", "tx-eth")}, nil)
	provider.On("GetTransactions", ctx, addr, "arbitrum", mock.Anything).
		Return([]pkgsync.DecodedTransaction{receiveTxOn("arbitrum", "tx-arb")}, nil)
	provider.On("GetTransactions", ctx, addr, "base", mock.Anything).
		Return([]pkgsync.DecodedTransaction(nil), errors.New("provider 503 on base"))

	storedChains := map[string]bool{}
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			raw := args.Get(1).(*pkgsync.RawTransaction)
			storedChains[raw.ChainID] = true
		}).Return(nil)

	cursorChains := map[string]bool{}
	walletRepo.On("SetChainCollectCursor", ctx, walletID, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { cursorChains[args.Get(2).(string)] = true }).
		Return(nil)

	erroredChains := map[string]string{}
	walletRepo.On("SetChainSyncError", ctx, walletID, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { erroredChains[args.Get(2).(string)] = args.Get(3).(string) }).
		Return(nil)

	collector := newTestCollector(provider, rawTxRepo, walletRepo, nil)
	count, err := collector.CollectAll(ctx, w)

	// A single chain's failure must NOT abort the whole collect.
	require.NoError(t, err)
	assert.Equal(t, 2, count, "eth + arbitrum txs stored; base contributed nothing")

	// Sibling chains recorded data and advanced their own cursors.
	assert.True(t, storedChains["ethereum"], "eth raw stored")
	assert.True(t, storedChains["arbitrum"], "arbitrum raw stored")
	assert.True(t, cursorChains["ethereum"], "eth cursor advanced")
	assert.True(t, cursorChains["arbitrum"], "arbitrum cursor advanced")

	// The failed chain is isolated: marked error, cursor NOT advanced.
	assert.Contains(t, erroredChains, "base", "base marked errored")
	assert.False(t, cursorChains["base"], "base cursor must NOT advance on failure")
	assert.False(t, storedChains["base"], "no base raw stored")

	// No sibling was marked errored by base's failure.
	assert.NotContains(t, erroredChains, "ethereum")
	assert.NotContains(t, erroredChains, "arbitrum")

	// The wallet-level collect cursor is no longer the incremental baseline; the
	// collector must not write it (per-chain cursors own resumption now).
	walletRepo.AssertNotCalled(t, "SetCollectCursor", ctx, walletID, mock.Anything)
}

// TestCollector_PerChainSince drives incremental collection and asserts each chain
// resumes from ITS OWN cursor, not a sibling's. eth is far ahead (T2); base failed
// last cycle and sits at T0. Both must be re-requested from their own cursor in the
// same cycle — proving no cross-chain cursor bleed and no skip.
func TestCollector_PerChainSince(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	addr := "0x2222222222222222222222222222222222222222"
	lastSync := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	w := &wallet.Wallet{ID: walletID, Address: addr, LastSyncAt: &lastSync}

	t0 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	rows := []wallet.WalletChainSync{
		chainRow(walletID, "base", &t0),
		chainRow(walletID, "ethereum", &t2),
	}
	walletRepo.On("GetChainSyncRows", ctx, walletID).Return(rows, nil)

	// Assert the exact `since` each chain is invoked with.
	sinceByChain := map[string]time.Time{}
	provider.On("GetTransactions", ctx, addr, "ethereum", mock.Anything).
		Run(func(args mock.Arguments) { sinceByChain["ethereum"] = args.Get(3).(time.Time) }).
		Return([]pkgsync.DecodedTransaction{}, nil)
	provider.On("GetTransactions", ctx, addr, "base", mock.Anything).
		Run(func(args mock.Arguments) { sinceByChain["base"] = args.Get(3).(time.Time) }).
		Return([]pkgsync.DecodedTransaction{}, nil)

	collector := newTestCollector(provider, rawTxRepo, walletRepo, nil)
	_, err := collector.CollectIncremental(ctx, w)
	require.NoError(t, err)

	assert.Equal(t, t2, sinceByChain["ethereum"], "eth resumes from its own cursor T2")
	assert.Equal(t, t0, sinceByChain["base"], "base resumes from its own cursor T0, not eth's T2")
}

// TestCollector_FailedChainResumesFromOwnCursor is the "no skip" guarantee across
// cycles. base has a cursor at T0 from a prior successful window. This cycle base
// errors, so its cursor stays T0 (verified: no SetChainCollectCursor for base).
// Combined with TestCollector_PerChainSince, that pins the invariant: base's next
// cycle re-requests from T0 — its history is never skipped because eth ran ahead.
func TestCollector_FailedChainResumesFromOwnCursor(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	addr := "0x3333333333333333333333333333333333333333"
	lastSync := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	w := &wallet.Wallet{ID: walletID, Address: addr, LastSyncAt: &lastSync}

	t0 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	rows := []wallet.WalletChainSync{
		chainRow(walletID, "base", &t0),
		chainRow(walletID, "ethereum", &t2),
	}
	walletRepo.On("GetChainSyncRows", ctx, walletID).Return(rows, nil)

	// eth advances; base errors this cycle.
	provider.On("GetTransactions", ctx, addr, "ethereum", mock.Anything).
		Return([]pkgsync.DecodedTransaction{receiveTxOn("ethereum", "tx-eth")}, nil)
	provider.On("GetTransactions", ctx, addr, "base", mock.Anything).
		Return([]pkgsync.DecodedTransaction(nil), errors.New("provider 503 on base"))

	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)

	baseCursorAdvanced := false
	walletRepo.On("SetChainCollectCursor", ctx, walletID, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			if args.Get(2).(string) == "base" {
				baseCursorAdvanced = true
			}
		}).Return(nil)
	walletRepo.On("SetChainSyncError", ctx, walletID, "base", mock.Anything).Return(nil)

	collector := newTestCollector(provider, rawTxRepo, walletRepo, nil)
	_, err := collector.CollectIncremental(ctx, w)

	require.NoError(t, err)
	assert.False(t, baseCursorAdvanced, "base cursor stays at T0 so next cycle resumes there, skipping nothing")
}

// TestReconciler_OneChainErrors_OthersReconcile mirrors the collector isolation on
// Phase 2: a per-chain GetPositions failure must isolate to that chain — mark it
// error, skip its genesis, and still synthesize genesis for the healthy chains.
func TestReconciler_OneChainErrors_OthersReconcile(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, "0x4444444444444444444444444444444444444444")
	t1 := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	walletRepo := new(MockWalletRepository)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, w.ID).Return(nil)
	rawTxRepo.On("GetAllByWallet", ctx, w.ID).Return([]*pkgsync.RawTransaction{}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, w.ID).Return(&t1, nil)

	rows := []wallet.WalletChainSync{
		chainRow(w.ID, "arbitrum", nil),
		chainRow(w.ID, "base", nil),
		chainRow(w.ID, "ethereum", nil),
	}
	walletRepo.On("GetChainSyncRows", ctx, w.ID).Return(rows, nil)

	// eth + arbitrum have balances; base errors.
	for _, c := range []string{"ethereum", "arbitrum"} {
		posProvider.On("GetPositions", ctx, w.Address, c).Return([]pkgsync.OnChainPosition{
			{ChainID: c, AssetSymbol: "ETH", Decimals: 18, Quantity: big.NewInt(1e18)},
		}, nil)
	}
	posProvider.On("GetPositions", ctx, w.Address, "base").
		Return([]pkgsync.OnChainPosition(nil), errors.New("positions 503 on base"))

	genesisChains := map[string]bool{}
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			raw := args.Get(1).(*pkgsync.RawTransaction)
			genesisChains[raw.ChainID] = true
		}).Return(nil)

	walletRepo.On("SetChainSyncError", ctx, w.ID, "base", mock.Anything).Return(nil)

	r := newTestReconciler(rawTxRepo, posProvider, walletRepo, nil)
	count, err := r.Reconcile(ctx, w)

	require.NoError(t, err, "one chain's position failure must not abort the whole reconcile")
	assert.Equal(t, 2, count, "genesis synthesized for eth + arbitrum only")
	assert.True(t, genesisChains["ethereum"])
	assert.True(t, genesisChains["arbitrum"])
	assert.False(t, genesisChains["base"], "no genesis for a chain whose balance failed to load")
	walletRepo.AssertCalled(t, "SetChainSyncError", ctx, w.ID, "base", mock.Anything)
}
