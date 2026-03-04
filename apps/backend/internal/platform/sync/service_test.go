package sync_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	pkgsync "github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Mock TransactionDataProvider
// =============================================================================

type MockTransactionDataProvider struct {
	mock.Mock
}

func (m *MockTransactionDataProvider) GetTransactions(ctx context.Context, address string, since time.Time) ([]pkgsync.DecodedTransaction, error) {
	args := m.Called(ctx, address, since)
	return args.Get(0).([]pkgsync.DecodedTransaction), args.Error(1)
}

var _ pkgsync.TransactionDataProvider = (*MockTransactionDataProvider)(nil)

// =============================================================================
// Helper to create a Service with mocks (3-phase pipeline)
// =============================================================================

func newTestService(
	walletRepo pkgsync.WalletRepository,
	ledgerSvc pkgsync.LedgerService,
	provider pkgsync.TransactionDataProvider,
	posProvider pkgsync.PositionDataProvider,
	rawTxRepo pkgsync.RawTransactionRepository,
) *pkgsync.Service {
	log := logger.New("test", os.Stdout)
	config := pkgsync.DefaultConfig()
	return pkgsync.NewService(config, walletRepo, ledgerSvc, nil, log, provider, posProvider, rawTxRepo, nil, nil, nil)
}

// marshalDecodedTx is a test helper to serialize a DecodedTransaction to JSON (for RawTransaction.RawJSON)
func marshalDecodedTx(dt pkgsync.DecodedTransaction) []byte {
	data, _ := json.Marshal(dt)
	return data
}

// =============================================================================
// Tests
// =============================================================================

// TestSyncWallet_InitialSync_ProcessesAllPhases tests the initial sync pipeline:
// Collect → Reconcile → Process
func TestSyncWallet_InitialSync_ProcessesAllPhases(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: nil, // Initial sync
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	// Wallet claim succeeds
	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)

	// --- Phase 1: Collect ---
	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	t1Time := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	t2Time := time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC)

	txReceive := pkgsync.DecodedTransaction{
		ID:            "tx-receive-1",
		TxHash:        "0xaaa",
		ChainID:       "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(1e18),
			Direction:   pkgsync.DirectionIn,
			Sender:      "0x9999999999999999999999999999999999999999",
			Recipient:   walletAddr,
		}},
		MinedAt: t1Time,
		Status:  "confirmed",
	}

	txReceive2 := pkgsync.DecodedTransaction{
		ID:            "tx-receive-2",
		TxHash:        "0xbbb",
		ChainID:       "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(2e18),
			Direction:   pkgsync.DirectionIn,
			Sender:      "0x8888888888888888888888888888888888888888",
			Recipient:   walletAddr,
		}},
		MinedAt: t2Time,
		Status:  "confirmed",
	}

	provider.On("GetTransactions", ctx, walletAddr, mock.Anything).
		Return([]pkgsync.DecodedTransaction{txReceive, txReceive2}, nil)

	// Collector stores raw transactions
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)

	// Collector updates cursor
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	// --- Phase 2: Reconcile ---
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, walletID).Return(nil)

	// Reconciler loads all raw transactions
	rawID1 := uuid.New()
	rawID2 := uuid.New()
	allRaws := []*pkgsync.RawTransaction{
		{ID: rawID1, WalletID: walletID, ZerionID: "tx-receive-1", TxHash: "0xaaa", ChainID: "ethereum", OperationType: "receive", MinedAt: t1Time, Status: "confirmed", RawJSON: marshalDecodedTx(txReceive), ProcessingStatus: pkgsync.ProcessingStatusPending},
		{ID: rawID2, WalletID: walletID, ZerionID: "tx-receive-2", TxHash: "0xbbb", ChainID: "ethereum", OperationType: "receive", MinedAt: t2Time, Status: "confirmed", RawJSON: marshalDecodedTx(txReceive2), ProcessingStatus: pkgsync.ProcessingStatusPending},
	}
	rawTxRepo.On("GetAllByWallet", ctx, walletID).Return(allRaws, nil)

	// On-chain shows exact balance match (3 ETH total = 1+2 ETH received) → no genesis needed
	posProvider.On("GetPositions", ctx, walletAddr).Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "ETH", Decimals: 18, Quantity: new(big.Int).Add(big.NewInt(1e18), big.NewInt(2e18))},
	}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, walletID).Return(&t1Time, nil)

	// --- Phase 3: Process ---
	rawTxRepo.On("GetPendingByWallet", ctx, walletID).Return(allRaws, nil)

	// Counterparty address lookups
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, userID).Return([]*wallet.Wallet{}, nil)

	// Both transactions succeed
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Processor marks raw transactions as processed
	rawTxRepo.On("MarkProcessed", ctx, mock.Anything, mock.Anything).Return(nil)

	// Cursor update after processing
	walletRepo.On("SetSyncCompletedAt", ctx, walletID, mock.Anything).Return(nil)

	// SyncWallet uses GetWalletsForSync
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider, posProvider, rawTxRepo)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Verify: 2 RecordTransaction calls
	ledgerSvc.AssertNumberOfCalls(t, "RecordTransaction", 2)
	walletRepo.AssertCalled(t, "SetSyncCompletedAt", ctx, walletID, mock.Anything)
}

// TestSyncWallet_IncrementalSync_CollectAndProcess tests incremental sync:
// Collect new → Process (no reconcile)
func TestSyncWallet_IncrementalSync_CollectAndProcess(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	lastSync := time.Date(2024, 6, 14, 0, 0, 0, 0, time.UTC)
	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: &lastSync, // Incremental sync
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)
	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	t1Time := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	txReceive := pkgsync.DecodedTransaction{
		ID:            "tx-receive-1",
		TxHash:        "0xaaa",
		ChainID:       "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(1e18),
			Direction:   pkgsync.DirectionIn,
			Sender:      "0x9999999999999999999999999999999999999999",
			Recipient:   walletAddr,
		}},
		MinedAt: t1Time,
		Status:  "confirmed",
	}

	// Collector fetches new transactions
	provider.On("GetTransactions", ctx, walletAddr, lastSync).
		Return([]pkgsync.DecodedTransaction{txReceive}, nil)
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	// Processor gets pending transactions
	rawID := uuid.New()
	pendingRaws := []*pkgsync.RawTransaction{
		{ID: rawID, WalletID: walletID, ZerionID: "tx-receive-1", TxHash: "0xaaa", ChainID: "ethereum", OperationType: "receive", MinedAt: t1Time, Status: "confirmed", RawJSON: marshalDecodedTx(txReceive), ProcessingStatus: pkgsync.ProcessingStatusPending},
	}
	rawTxRepo.On("GetPendingByWallet", ctx, walletID).Return(pendingRaws, nil)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, userID).Return([]*wallet.Wallet{}, nil)

	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	rawTxRepo.On("MarkProcessed", ctx, mock.Anything, mock.Anything).Return(nil)
	walletRepo.On("SetSyncCompletedAt", ctx, walletID, mock.Anything).Return(nil)
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider, posProvider, rawTxRepo)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Verify: 1 transaction processed
	ledgerSvc.AssertNumberOfCalls(t, "RecordTransaction", 1)

	// Verify reconcile was NOT called (incremental sync skips it)
	posProvider.AssertNotCalled(t, "GetPositions")
	rawTxRepo.AssertNotCalled(t, "GetAllByWallet")
}

// TestSyncWallet_TransactionsProcessedOldestFirst verifies chronological ordering
// in the Processor despite Zerion returning newest-first
func TestSyncWallet_TransactionsProcessedOldestFirst(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: nil, // Initial sync
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)
	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	t1Time := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC) // oldest
	t2Time := time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC)
	t3Time := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC) // newest

	tx1 := pkgsync.DecodedTransaction{
		ID: "tx-1", TxHash: "0xaaa", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(1e18),
			Direction: pkgsync.DirectionIn,
			Sender:    "0x9999999999999999999999999999999999999999", Recipient: walletAddr,
		}},
		MinedAt: t1Time, Status: "confirmed",
	}
	tx2 := pkgsync.DecodedTransaction{
		ID: "tx-2", TxHash: "0xbbb", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(2e18),
			Direction: pkgsync.DirectionIn,
			Sender:    "0x9999999999999999999999999999999999999999", Recipient: walletAddr,
		}},
		MinedAt: t2Time, Status: "confirmed",
	}
	tx3 := pkgsync.DecodedTransaction{
		ID: "tx-3", TxHash: "0xccc", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(3e18),
			Direction: pkgsync.DirectionIn,
			Sender:    "0x9999999999999999999999999999999999999999", Recipient: walletAddr,
		}},
		MinedAt: t3Time, Status: "confirmed",
	}

	// Provider returns newest-first (like Zerion)
	provider.On("GetTransactions", ctx, walletAddr, mock.Anything).
		Return([]pkgsync.DecodedTransaction{tx3, tx2, tx1}, nil)
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	// Reconcile: no genesis needed (exact balance match)
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, walletID).Return(nil)
	totalETH := new(big.Int).Add(big.NewInt(1e18), big.NewInt(2e18))
	totalETH.Add(totalETH, big.NewInt(3e18))
	posProvider.On("GetPositions", ctx, walletAddr).Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "ETH", Decimals: 18, Quantity: totalETH},
	}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, walletID).Return(&t1Time, nil)

	// GetAllByWallet for reconcile (return in reverse order to prove Processor re-sorts)
	allRaws := []*pkgsync.RawTransaction{
		{ID: uuid.New(), WalletID: walletID, ZerionID: "tx-3", TxHash: "0xccc", ChainID: "ethereum", OperationType: "receive", MinedAt: t3Time, Status: "confirmed", RawJSON: marshalDecodedTx(tx3), ProcessingStatus: pkgsync.ProcessingStatusPending},
		{ID: uuid.New(), WalletID: walletID, ZerionID: "tx-2", TxHash: "0xbbb", ChainID: "ethereum", OperationType: "receive", MinedAt: t2Time, Status: "confirmed", RawJSON: marshalDecodedTx(tx2), ProcessingStatus: pkgsync.ProcessingStatusPending},
		{ID: uuid.New(), WalletID: walletID, ZerionID: "tx-1", TxHash: "0xaaa", ChainID: "ethereum", OperationType: "receive", MinedAt: t1Time, Status: "confirmed", RawJSON: marshalDecodedTx(tx1), ProcessingStatus: pkgsync.ProcessingStatusPending},
	}
	rawTxRepo.On("GetAllByWallet", ctx, walletID).Return(allRaws, nil)

	// GetPendingByWallet for processing (return in reverse order — Processor must sort)
	rawTxRepo.On("GetPendingByWallet", ctx, walletID).Return(allRaws, nil)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, userID).Return([]*wallet.Wallet{}, nil)

	// Track the order of external IDs processed
	var processedOrder []string
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			id := args.Get(3).(*string)
			processedOrder = append(processedOrder, *id)
		}).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	rawTxRepo.On("MarkProcessed", ctx, mock.Anything, mock.Anything).Return(nil)
	walletRepo.On("SetSyncCompletedAt", ctx, walletID, mock.Anything).Return(nil)
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider, posProvider, rawTxRepo)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Verify transactions were processed oldest-first despite being returned newest-first
	require.Len(t, processedOrder, 3)
	assert.Equal(t, "tx-1", processedOrder[0], "oldest transaction should be processed first")
	assert.Equal(t, "tx-2", processedOrder[1])
	assert.Equal(t, "tx-3", processedOrder[2], "newest transaction should be processed last")
}

// TestSyncWallet_InitialSync_ReconcileCreatesGenesis tests that reconciliation
// creates a genesis raw transaction when on-chain balance > calculated flows
func TestSyncWallet_InitialSync_ReconcileCreatesGenesis(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: nil, // Initial sync
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)
	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	t1Time := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	// We only have a send of 1 USDC, but on-chain shows 2 USDC → genesis needed for 3 USDC (send+remaining)
	txSend := pkgsync.DecodedTransaction{
		ID:            "tx-send-1",
		TxHash:        "0xaaa",
		ChainID:       "ethereum",
		OperationType: pkgsync.OpSend,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "USDC",
			Decimals:    6,
			Amount:      big.NewInt(1_000_000), // 1 USDC out
			Direction:   pkgsync.DirectionOut,
			Sender:      walletAddr,
			Recipient:   "0x9999999999999999999999999999999999999999",
		}},
		MinedAt: t1Time,
		Status:  "confirmed",
	}

	// Phase 1: Collect
	provider.On("GetTransactions", ctx, walletAddr, mock.Anything).
		Return([]pkgsync.DecodedTransaction{txSend}, nil)
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	// Phase 2: Reconcile
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, walletID).Return(nil)
	sendRaw := &pkgsync.RawTransaction{
		ID: uuid.New(), WalletID: walletID, ZerionID: "tx-send-1", TxHash: "0xaaa",
		ChainID: "ethereum", OperationType: "send", MinedAt: t1Time, Status: "confirmed",
		RawJSON: marshalDecodedTx(txSend), ProcessingStatus: pkgsync.ProcessingStatusPending,
	}
	rawTxRepo.On("GetAllByWallet", ctx, walletID).Return([]*pkgsync.RawTransaction{sendRaw}, nil)

	// On-chain shows 2 USDC. Net flow is -1 USDC (outflow). Delta = 2 - (-1) = 3 USDC genesis needed.
	posProvider.On("GetPositions", ctx, walletAddr).Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "USDC", Decimals: 6, Quantity: big.NewInt(2_000_000)},
	}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, walletID).Return(&t1Time, nil)

	// Phase 3: Process — return genesis + send in chronological order
	genesisTime := t1Time.Add(-1 * time.Second)
	genesisRaw := &pkgsync.RawTransaction{
		ID: uuid.New(), WalletID: walletID,
		ZerionID:  fmt.Sprintf("genesis:%s:ethereum:USDC", walletID.String()),
		TxHash:    "genesis_ethereum_USDC", ChainID: "ethereum",
		OperationType: "receive", MinedAt: genesisTime, Status: "confirmed",
		ProcessingStatus: pkgsync.ProcessingStatusPending,
		IsSynthetic:      true,
	}
	// Build the genesis RawJSON
	genesisTx := pkgsync.DecodedTransaction{
		ID:            genesisRaw.ZerionID,
		TxHash:        genesisRaw.TxHash,
		ChainID:       "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "USDC", Decimals: 6,
			Amount:    big.NewInt(3_000_000), // delta
			Direction: pkgsync.DirectionIn,
		}},
		MinedAt: genesisTime,
		Status:  "confirmed",
	}
	genesisRaw.RawJSON = marshalDecodedTx(genesisTx)

	rawTxRepo.On("GetPendingByWallet", ctx, walletID).Return([]*pkgsync.RawTransaction{genesisRaw, sendRaw}, nil)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, userID).Return([]*wallet.Wallet{}, nil)

	// Genesis is processed as genesis_balance
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeGenesisBalance, "sync_genesis", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Send is processed as transfer_out
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	rawTxRepo.On("MarkProcessed", ctx, mock.Anything, mock.Anything).Return(nil)
	walletRepo.On("SetSyncCompletedAt", ctx, walletID, mock.Anything).Return(nil)
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider, posProvider, rawTxRepo)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Verify: genesis + send = 2 RecordTransaction calls
	ledgerSvc.AssertNumberOfCalls(t, "RecordTransaction", 2)
	ledgerSvc.AssertCalled(t, "RecordTransaction", ctx, ledger.TxTypeGenesisBalance, "sync_genesis", mock.Anything, mock.Anything, mock.Anything)
	ledgerSvc.AssertCalled(t, "RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.Anything, mock.Anything, mock.Anything)
}

// TestSyncWallet_ConsecutiveErrors_StopsAfterThreshold tests that the Processor
// stops after too many consecutive errors
func TestSyncWallet_ConsecutiveErrors_StopsAfterThreshold(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: nil,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)
	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	// Create 8 transactions
	var txs []pkgsync.DecodedTransaction
	var pendingRaws []*pkgsync.RawTransaction
	for i := 0; i < 8; i++ {
		dt := pkgsync.DecodedTransaction{
			ID:            fmt.Sprintf("tx-%d", i),
			TxHash:        fmt.Sprintf("0x%d", i),
			ChainID:       "ethereum",
			OperationType: pkgsync.OpReceive,
			Transfers: []pkgsync.DecodedTransfer{{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(1e18),
				Direction:   pkgsync.DirectionIn,
				Sender:      "0x9999999999999999999999999999999999999999",
				Recipient:   walletAddr,
			}},
			MinedAt: time.Date(2024, 6, 15, 10+i, 0, 0, 0, time.UTC),
			Status:  "confirmed",
		}
		txs = append(txs, dt)
		pendingRaws = append(pendingRaws, &pkgsync.RawTransaction{
			ID: uuid.New(), WalletID: walletID, ZerionID: dt.ID, TxHash: dt.TxHash,
			ChainID: "ethereum", OperationType: "receive",
			MinedAt: dt.MinedAt, Status: "confirmed",
			RawJSON: marshalDecodedTx(dt), ProcessingStatus: pkgsync.ProcessingStatusPending,
		})
	}

	// Phase 1: Collect
	provider.On("GetTransactions", ctx, walletAddr, mock.Anything).Return(txs, nil)
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	// Phase 2: Reconcile
	rawTxRepo.On("DeleteSyntheticByWallet", ctx, walletID).Return(nil)
	rawTxRepo.On("GetAllByWallet", ctx, walletID).Return(pendingRaws, nil)
	totalETH := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(8))
	posProvider.On("GetPositions", ctx, walletAddr).Return([]pkgsync.OnChainPosition{
		{ChainID: "ethereum", AssetSymbol: "ETH", Decimals: 18, Quantity: totalETH},
	}, nil)
	rawTxRepo.On("GetEarliestMinedAt", ctx, walletID).Return(&txs[0].MinedAt, nil)

	// Phase 3: Process — all fail
	rawTxRepo.On("GetPendingByWallet", ctx, walletID).Return(pendingRaws, nil)
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, userID).Return([]*wallet.Wallet{}, nil)

	// All RecordTransaction calls fail with a generic error
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("database connection error"))

	rawTxRepo.On("MarkError", ctx, mock.Anything, mock.Anything).Return(nil)
	walletRepo.On("SetSyncError", ctx, walletID, mock.Anything).Return(nil)
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider, posProvider, rawTxRepo)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Should stop after 6 consecutive errors (threshold is >5)
	ledgerSvc.AssertNumberOfCalls(t, "RecordTransaction", 6)
}

// TestSyncWallet_ProcessorSkipsErrorsAndContinues tests that errors in individual
// transactions don't stop the entire sync, and the cursor advances past successes
func TestSyncWallet_ProcessorSkipsErrorsAndContinues(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	lastSync := time.Date(2024, 6, 14, 0, 0, 0, 0, time.UTC)
	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: &lastSync,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)
	posProvider := new(MockPositionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)

	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)
	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)

	t1Time := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	t2Time := time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC)
	t3Time := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	txReceive1 := pkgsync.DecodedTransaction{
		ID: "tx-receive-1", TxHash: "0xaaa", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(1e18),
			Direction: pkgsync.DirectionIn,
			Sender: "0x9999999999999999999999999999999999999999", Recipient: walletAddr,
		}},
		MinedAt: t1Time, Status: "confirmed",
	}
	txSend := pkgsync.DecodedTransaction{
		ID: "tx-send", TxHash: "0xbbb", ChainID: "ethereum",
		OperationType: pkgsync.OpSend,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "USDC", Decimals: 6, Amount: big.NewInt(1_000_000),
			Direction: pkgsync.DirectionOut,
			Sender: walletAddr, Recipient: "0x9999999999999999999999999999999999999999",
		}},
		MinedAt: t2Time, Status: "confirmed",
	}
	txReceive2 := pkgsync.DecodedTransaction{
		ID: "tx-receive-2", TxHash: "0xccc", ChainID: "ethereum",
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(2e18),
			Direction: pkgsync.DirectionIn,
			Sender: "0x8888888888888888888888888888888888888888", Recipient: walletAddr,
		}},
		MinedAt: t3Time, Status: "confirmed",
	}

	// Collect
	provider.On("GetTransactions", ctx, walletAddr, lastSync).
		Return([]pkgsync.DecodedTransaction{txReceive1, txSend, txReceive2}, nil)
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetCollectCursor", ctx, walletID, mock.Anything).Return(nil)

	// Process (incremental — no reconcile)
	rawID1 := uuid.New()
	rawID2 := uuid.New()
	rawID3 := uuid.New()
	pendingRaws := []*pkgsync.RawTransaction{
		{ID: rawID1, WalletID: walletID, ZerionID: "tx-receive-1", TxHash: "0xaaa", ChainID: "ethereum", OperationType: "receive", MinedAt: t1Time, Status: "confirmed", RawJSON: marshalDecodedTx(txReceive1), ProcessingStatus: pkgsync.ProcessingStatusPending},
		{ID: rawID2, WalletID: walletID, ZerionID: "tx-send", TxHash: "0xbbb", ChainID: "ethereum", OperationType: "send", MinedAt: t2Time, Status: "confirmed", RawJSON: marshalDecodedTx(txSend), ProcessingStatus: pkgsync.ProcessingStatusPending},
		{ID: rawID3, WalletID: walletID, ZerionID: "tx-receive-2", TxHash: "0xccc", ChainID: "ethereum", OperationType: "receive", MinedAt: t3Time, Status: "confirmed", RawJSON: marshalDecodedTx(txReceive2), ProcessingStatus: pkgsync.ProcessingStatusPending},
	}
	rawTxRepo.On("GetPendingByWallet", ctx, walletID).Return(pendingRaws, nil)
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, mock.Anything, userID).Return([]*wallet.Wallet{}, nil)

	// First receive succeeds
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-receive-1"
	}), mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Send fails with negative balance error
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-send"
	}), mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("failed to record transaction: account ACC would have negative balance for USDC"))

	// Third receive succeeds (processing continues past the error)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-receive-2"
	}), mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	rawTxRepo.On("MarkProcessed", ctx, mock.Anything, mock.Anything).Return(nil)
	rawTxRepo.On("MarkError", ctx, rawID2, mock.Anything).Return(nil)
	walletRepo.On("SetSyncCompletedAt", ctx, walletID, mock.Anything).Return(nil)
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider, posProvider, rawTxRepo)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Verify: 3 RecordTransaction calls (receive ok + send fail + receive ok)
	ledgerSvc.AssertNumberOfCalls(t, "RecordTransaction", 3)
	walletRepo.AssertCalled(t, "SetSyncCompletedAt", ctx, walletID, mock.Anything)
}
