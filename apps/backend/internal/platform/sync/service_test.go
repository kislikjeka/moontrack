package sync_test

import (
	"context"
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

func (m *MockTransactionDataProvider) GetTransactions(ctx context.Context, address string, chainID int64, since time.Time) ([]pkgsync.DecodedTransaction, error) {
	args := m.Called(ctx, address, chainID, since)
	return args.Get(0).([]pkgsync.DecodedTransaction), args.Error(1)
}

var _ pkgsync.TransactionDataProvider = (*MockTransactionDataProvider)(nil)

// =============================================================================
// Helper to create a Service with mocks
// =============================================================================

func newTestService(
	walletRepo pkgsync.WalletRepository,
	ledgerSvc pkgsync.LedgerService,
	provider pkgsync.TransactionDataProvider,
) *pkgsync.Service {
	log := logger.New("test", os.Stdout)
	config := pkgsync.DefaultConfig()
	return pkgsync.NewService(config, walletRepo, ledgerSvc, nil, log, provider)
}

// =============================================================================
// Tests
// =============================================================================

func TestSyncWallet_InitialSync_SkipsNegativeBalanceErrors(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		ChainID:    1,
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: nil, // Initial sync
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)

	// Wallet claim succeeds
	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)

	// Return 3 transactions: receive (ok), send (negative balance), receive (ok)
	t1Time := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	t2Time := time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC)
	t3Time := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	txReceive1 := pkgsync.DecodedTransaction{
		ID:            "tx-receive-1",
		TxHash:        "0xaaa",
		ChainID:       1,
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

	txSend := pkgsync.DecodedTransaction{
		ID:            "tx-send",
		TxHash:        "0xbbb",
		ChainID:       1,
		OperationType: pkgsync.OpSend,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "USDC",
			Decimals:    6,
			Amount:      big.NewInt(1000000),
			Direction:   pkgsync.DirectionOut,
			Sender:      walletAddr,
			Recipient:   "0x9999999999999999999999999999999999999999",
		}},
		MinedAt: t2Time,
		Status:  "confirmed",
	}

	txReceive2 := pkgsync.DecodedTransaction{
		ID:            "tx-receive-2",
		TxHash:        "0xccc",
		ChainID:       1,
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(2e18),
			Direction:   pkgsync.DirectionIn,
			Sender:      "0x8888888888888888888888888888888888888888",
			Recipient:   walletAddr,
		}},
		MinedAt: t3Time,
		Status:  "confirmed",
	}

	// Provider returns transactions (already oldest-first for simplicity)
	provider.On("GetTransactions", ctx, walletAddr, int64(1), mock.Anything).
		Return([]pkgsync.DecodedTransaction{txReceive1, txSend, txReceive2}, nil)

	// Counterparty address lookups (for internal transfer detection)
	externalAddr1 := "0x9999999999999999999999999999999999999999"
	externalAddr2 := "0x8888888888888888888888888888888888888888"
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr1, userID).Return([]*wallet.Wallet{}, nil)
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr2, userID).Return([]*wallet.Wallet{}, nil)

	// First receive succeeds
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-receive-1"
	}), mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Send fails with negative balance
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-send"
	}), mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("failed to record transaction: account ACC would have negative balance for USDC: current=0, change=-1000000, new=-1000000"))

	// Third receive succeeds
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-receive-2"
	}), mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Cursor should advance to t3Time (last successful tx)
	walletRepo.On("SetSyncCompletedAt", ctx, walletID, t3Time).Return(nil)

	// SyncWallet uses GetWalletsForSync to find the wallet
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Verify: 2 successful + 1 skipped = 3 RecordTransaction calls
	ledgerSvc.AssertNumberOfCalls(t, "RecordTransaction", 3)
	walletRepo.AssertCalled(t, "SetSyncCompletedAt", ctx, walletID, t3Time)
}

func TestSyncWallet_IncrementalSync_StopsOnNegativeBalanceError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	lastSync := time.Date(2024, 6, 14, 0, 0, 0, 0, time.UTC)
	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		ChainID:    1,
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: &lastSync, // Incremental sync
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)

	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)

	t1Time := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	t2Time := time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC)
	t3Time := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	txReceive := pkgsync.DecodedTransaction{
		ID:            "tx-receive-1",
		TxHash:        "0xaaa",
		ChainID:       1,
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

	txSend := pkgsync.DecodedTransaction{
		ID:            "tx-send",
		TxHash:        "0xbbb",
		ChainID:       1,
		OperationType: pkgsync.OpSend,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "USDC",
			Decimals:    6,
			Amount:      big.NewInt(1000000),
			Direction:   pkgsync.DirectionOut,
			Sender:      walletAddr,
			Recipient:   "0x9999999999999999999999999999999999999999",
		}},
		MinedAt: t2Time,
		Status:  "confirmed",
	}

	txReceive2 := pkgsync.DecodedTransaction{
		ID:            "tx-receive-2",
		TxHash:        "0xccc",
		ChainID:       1,
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(2e18),
			Direction:   pkgsync.DirectionIn,
			Sender:      "0x8888888888888888888888888888888888888888",
			Recipient:   walletAddr,
		}},
		MinedAt: t3Time,
		Status:  "confirmed",
	}

	provider.On("GetTransactions", ctx, walletAddr, int64(1), lastSync).
		Return([]pkgsync.DecodedTransaction{txReceive, txSend, txReceive2}, nil)

	externalAddr := "0x9999999999999999999999999999999999999999"
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)

	// First receive succeeds
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-receive-1"
	}), mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Send fails with negative balance — incremental sync should STOP here
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-send"
	}), mock.Anything, mock.Anything).
		Return(nil, fmt.Errorf("failed to record transaction: account ACC would have negative balance for USDC: current=0, change=-1000000, new=-1000000"))

	// Cursor advances to t1Time (last successful before error)
	walletRepo.On("SetSyncCompletedAt", ctx, walletID, t1Time).Return(nil)

	// SyncWallet uses GetWalletsForSync to find the wallet
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Verify: only 2 RecordTransaction calls (receive + failed send), NOT 3
	ledgerSvc.AssertNumberOfCalls(t, "RecordTransaction", 2)
	// Third tx should NOT have been attempted
	ledgerSvc.AssertNotCalled(t, "RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.MatchedBy(func(id *string) bool {
		return id != nil && *id == "tx-receive-2"
	}), mock.Anything, mock.Anything)
	// Cursor should advance to t1Time, not t3Time
	walletRepo.AssertCalled(t, "SetSyncCompletedAt", ctx, walletID, t1Time)
}

func TestSyncWallet_TransactionsProcessedOldestFirst(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletID := uuid.New()
	walletAddr := "0x1111111111111111111111111111111111111111"

	w := &wallet.Wallet{
		ID:         walletID,
		UserID:     userID,
		Name:       "Test Wallet",
		ChainID:    1,
		Address:    walletAddr,
		SyncStatus: wallet.SyncStatusPending,
		LastSyncAt: nil, // Initial sync
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	provider := new(MockTransactionDataProvider)

	walletRepo.On("ClaimWalletForSync", ctx, walletID).Return(true, nil)

	// Return transactions in reverse order (newest-first, like Zerion API)
	t1Time := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC) // oldest
	t2Time := time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC)
	t3Time := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC) // newest

	tx3 := pkgsync.DecodedTransaction{
		ID: "tx-3", TxHash: "0xccc", ChainID: 1,
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(3e18),
			Direction: pkgsync.DirectionIn,
			Sender:    "0x9999999999999999999999999999999999999999", Recipient: walletAddr,
		}},
		MinedAt: t3Time, Status: "confirmed",
	}
	tx2 := pkgsync.DecodedTransaction{
		ID: "tx-2", TxHash: "0xbbb", ChainID: 1,
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(2e18),
			Direction: pkgsync.DirectionIn,
			Sender:    "0x9999999999999999999999999999999999999999", Recipient: walletAddr,
		}},
		MinedAt: t2Time, Status: "confirmed",
	}
	tx1 := pkgsync.DecodedTransaction{
		ID: "tx-1", TxHash: "0xaaa", ChainID: 1,
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", Decimals: 18, Amount: big.NewInt(1e18),
			Direction: pkgsync.DirectionIn,
			Sender:    "0x9999999999999999999999999999999999999999", Recipient: walletAddr,
		}},
		MinedAt: t1Time, Status: "confirmed",
	}

	// Provider returns newest-first (like Zerion)
	provider.On("GetTransactions", ctx, walletAddr, int64(1), mock.Anything).
		Return([]pkgsync.DecodedTransaction{tx3, tx2, tx1}, nil)

	externalAddr := "0x9999999999999999999999999999999999999999"
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddr, userID).Return([]*wallet.Wallet{}, nil)

	// Track the order of external IDs processed
	var processedOrder []string
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "zerion", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			id := args.Get(3).(*string)
			processedOrder = append(processedOrder, *id)
		}).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	walletRepo.On("SetSyncCompletedAt", ctx, walletID, t3Time).Return(nil)

	// SyncWallet uses GetWalletsForSync
	walletRepo.On("GetWalletsForSync", ctx).Return([]*wallet.Wallet{w}, nil)

	svc := newTestService(walletRepo, ledgerSvc, provider)
	err := svc.SyncWallet(ctx, walletID)
	require.NoError(t, err)

	// Verify transactions were processed oldest-first despite being returned newest-first
	require.Len(t, processedOrder, 3)
	assert.Equal(t, "tx-1", processedOrder[0], "oldest transaction should be processed first")
	assert.Equal(t, "tx-2", processedOrder[1])
	assert.Equal(t, "tx-3", processedOrder[2], "newest transaction should be processed last")
}
