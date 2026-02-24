package sync_test

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// =============================================================================
// Mock Wallet Repository
// =============================================================================

type MockWalletRepository struct {
	mock.Mock
}

func (m *MockWalletRepository) GetWalletsForSync(ctx context.Context) ([]*wallet.Wallet, error) {
	args := m.Called(ctx)
	return args.Get(0).([]*wallet.Wallet), args.Error(1)
}

func (m *MockWalletRepository) GetWalletsByAddressAndUserID(ctx context.Context, address string, userID uuid.UUID) ([]*wallet.Wallet, error) {
	args := m.Called(ctx, address, userID)
	return args.Get(0).([]*wallet.Wallet), args.Error(1)
}

func (m *MockWalletRepository) ClaimWalletForSync(ctx context.Context, walletID uuid.UUID) (bool, error) {
	args := m.Called(ctx, walletID)
	return args.Bool(0), args.Error(1)
}

func (m *MockWalletRepository) SetSyncInProgress(ctx context.Context, walletID uuid.UUID) error {
	args := m.Called(ctx, walletID)
	return args.Error(0)
}

func (m *MockWalletRepository) SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error {
	args := m.Called(ctx, walletID, syncAt)
	return args.Error(0)
}

func (m *MockWalletRepository) SetSyncError(ctx context.Context, walletID uuid.UUID, errMsg string) error {
	args := m.Called(ctx, walletID, errMsg)
	return args.Error(0)
}

func (m *MockWalletRepository) SetSyncPhase(ctx context.Context, walletID uuid.UUID, phase string) error {
	args := m.Called(ctx, walletID, phase)
	return args.Error(0)
}

func (m *MockWalletRepository) SetCollectCursor(ctx context.Context, walletID uuid.UUID, cursor time.Time) error {
	args := m.Called(ctx, walletID, cursor)
	return args.Error(0)
}

func (m *MockWalletRepository) WipeWalletLedger(ctx context.Context, walletID uuid.UUID) error {
	args := m.Called(ctx, walletID)
	return args.Error(0)
}

// =============================================================================
// Mock Ledger Service
// =============================================================================

type recordedTransaction struct {
	TxType     ledger.TransactionType
	Source     string
	ExternalID *string
	OccurredAt time.Time
	RawData    map[string]interface{}
}

type MockLedgerService struct {
	mock.Mock
	recordedTransactions []recordedTransaction
}

func (m *MockLedgerService) RecordTransaction(ctx context.Context, transactionType ledger.TransactionType, source string, externalID *string, occurredAt time.Time, rawData map[string]interface{}) (*ledger.Transaction, error) {
	args := m.Called(ctx, transactionType, source, externalID, occurredAt, rawData)

	m.recordedTransactions = append(m.recordedTransactions, recordedTransaction{
		TxType:     transactionType,
		Source:     source,
		ExternalID: externalID,
		OccurredAt: occurredAt,
		RawData:    rawData,
	})

	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ledger.Transaction), args.Error(1)
}

// =============================================================================
// Mock Asset Service
// =============================================================================

type MockAssetService struct {
	mock.Mock
}

func (m *MockAssetService) GetPriceBySymbol(ctx context.Context, symbol string) (*interface{}, error) {
	args := m.Called(ctx, symbol)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interface{}), args.Error(1)
}

// =============================================================================
// Mock RawTransactionRepository
// =============================================================================

type MockRawTransactionRepository struct {
	mock.Mock
}

func (m *MockRawTransactionRepository) UpsertRawTransaction(ctx context.Context, raw *sync.RawTransaction) error {
	args := m.Called(ctx, raw)
	return args.Error(0)
}

func (m *MockRawTransactionRepository) GetPendingByWallet(ctx context.Context, walletID uuid.UUID) ([]*sync.RawTransaction, error) {
	args := m.Called(ctx, walletID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*sync.RawTransaction), args.Error(1)
}

func (m *MockRawTransactionRepository) GetAllByWallet(ctx context.Context, walletID uuid.UUID) ([]*sync.RawTransaction, error) {
	args := m.Called(ctx, walletID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*sync.RawTransaction), args.Error(1)
}

func (m *MockRawTransactionRepository) MarkProcessed(ctx context.Context, rawID uuid.UUID, ledgerTxID uuid.UUID) error {
	args := m.Called(ctx, rawID, ledgerTxID)
	return args.Error(0)
}

func (m *MockRawTransactionRepository) MarkSkipped(ctx context.Context, rawID uuid.UUID, reason string) error {
	args := m.Called(ctx, rawID, reason)
	return args.Error(0)
}

func (m *MockRawTransactionRepository) MarkError(ctx context.Context, rawID uuid.UUID, errMsg string) error {
	args := m.Called(ctx, rawID, errMsg)
	return args.Error(0)
}

func (m *MockRawTransactionRepository) ResetProcessingStatus(ctx context.Context, walletID uuid.UUID) error {
	args := m.Called(ctx, walletID)
	return args.Error(0)
}

func (m *MockRawTransactionRepository) GetEarliestMinedAt(ctx context.Context, walletID uuid.UUID) (*time.Time, error) {
	args := m.Called(ctx, walletID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*time.Time), args.Error(1)
}

func (m *MockRawTransactionRepository) DeleteSyntheticByWallet(ctx context.Context, walletID uuid.UUID) error {
	args := m.Called(ctx, walletID)
	return args.Error(0)
}

// =============================================================================
// Mock PositionDataProvider
// =============================================================================

type MockPositionDataProvider struct {
	mock.Mock
}

func (m *MockPositionDataProvider) GetPositions(ctx context.Context, address string) ([]sync.OnChainPosition, error) {
	args := m.Called(ctx, address)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]sync.OnChainPosition), args.Error(1)
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTestWallet(userID uuid.UUID, address string) *wallet.Wallet {
	return &wallet.Wallet{
		ID:         uuid.New(),
		UserID:     userID,
		Name:       "Test Wallet",
		Address:    address,
		SyncStatus: wallet.SyncStatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// Ensure mocks implement the interfaces
var _ sync.WalletRepository = (*MockWalletRepository)(nil)
var _ sync.LedgerService = (*MockLedgerService)(nil)
var _ sync.RawTransactionRepository = (*MockRawTransactionRepository)(nil)
var _ sync.PositionDataProvider = (*MockPositionDataProvider)(nil)
