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

func (m *MockAssetService) GetPriceBySymbol(ctx context.Context, symbol string, chainID int64) (*interface{}, error) {
	args := m.Called(ctx, symbol, chainID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interface{}), args.Error(1)
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTestWallet(userID uuid.UUID, address string, chainID int64) *wallet.Wallet {
	return &wallet.Wallet{
		ID:         uuid.New(),
		UserID:     userID,
		Name:       "Test Wallet",
		ChainID:    chainID,
		Address:    address,
		SyncStatus: wallet.SyncStatusPending,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// Ensure mocks implement the interfaces
var _ sync.WalletRepository = (*MockWalletRepository)(nil)
var _ sync.LedgerService = (*MockLedgerService)(nil)
