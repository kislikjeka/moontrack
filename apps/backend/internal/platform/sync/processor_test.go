package sync_test

import (
	"context"
	"log/slog"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// =============================================================================
// Mocks
// =============================================================================

// MockWalletRepository mocks the WalletRepository interface
type MockWalletRepository struct {
	mock.Mock
}

func (m *MockWalletRepository) GetWalletsForSync(ctx context.Context) ([]*wallet.Wallet, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*wallet.Wallet), args.Error(1)
}

func (m *MockWalletRepository) GetWalletsByAddress(ctx context.Context, address string) ([]*wallet.Wallet, error) {
	args := m.Called(ctx, address)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*wallet.Wallet), args.Error(1)
}

func (m *MockWalletRepository) GetWalletsByAddressAndUserID(ctx context.Context, address string, userID uuid.UUID) ([]*wallet.Wallet, error) {
	args := m.Called(ctx, address, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
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

func (m *MockWalletRepository) SetSyncCompleted(ctx context.Context, walletID uuid.UUID, lastBlock int64, syncAt time.Time) error {
	args := m.Called(ctx, walletID, lastBlock, syncAt)
	return args.Error(0)
}

func (m *MockWalletRepository) SetSyncError(ctx context.Context, walletID uuid.UUID, errMsg string) error {
	args := m.Called(ctx, walletID, errMsg)
	return args.Error(0)
}

// MockLedgerService mocks the LedgerService interface
type MockLedgerService struct {
	mock.Mock
	recordedTransactions []recordedTx
}

type recordedTx struct {
	TxType     ledger.TransactionType
	Source     string
	ExternalID *string
	OccurredAt time.Time
	RawData    map[string]interface{}
}

func (m *MockLedgerService) RecordTransaction(ctx context.Context, transactionType ledger.TransactionType, source string, externalID *string, occurredAt time.Time, rawData map[string]interface{}) (*ledger.Transaction, error) {
	args := m.Called(ctx, transactionType, source, externalID, occurredAt, rawData)

	// Track the call for assertions
	m.recordedTransactions = append(m.recordedTransactions, recordedTx{
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

// MockAssetService mocks the AssetService interface
type MockAssetService struct {
	mock.Mock
}

func (m *MockAssetService) GetPriceBySymbol(ctx context.Context, symbol string, chainID int64) (*big.Int, error) {
	args := m.Called(ctx, symbol, chainID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*big.Int), args.Error(1)
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTestProcessor(walletRepo sync.WalletRepository, ledgerSvc sync.LedgerService, assetSvc sync.AssetService) *sync.Processor {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return sync.NewProcessor(walletRepo, ledgerSvc, assetSvc, logger)
}

func newTestWallet(userID uuid.UUID, address string, chainID int64) *wallet.Wallet {
	return &wallet.Wallet{
		ID:      uuid.New(),
		UserID:  userID,
		Name:    "Test Wallet",
		ChainID: chainID,
		Address: address,
	}
}

func newTestTransfer(from, to string, amount int64, direction sync.TransferDirection) sync.Transfer {
	return sync.Transfer{
		TxHash:          "0x" + uuid.New().String()[:32],
		BlockNumber:     12345678,
		Timestamp:       time.Now().Add(-time.Hour),
		From:            from,
		To:              to,
		Amount:          big.NewInt(amount),
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		ChainID:         1,
		Direction:       direction,
		TransferType:    sync.TransferTypeExternal,
		UniqueID:        "unique-" + uuid.New().String()[:8],
	}
}

// =============================================================================
// Classification Tests
// =============================================================================

func TestProcessor_ClassifyTransfer_Incoming(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddress := "0x1111111111111111111111111111111111111111"
	externalAddress := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	// External address is not a user wallet (user-scoped)
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddress, userID).Return([]*wallet.Wallet{}, nil)

	// Mock ledger to record incoming transfer
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Mock asset price
	assetSvc.On("GetPriceBySymbol", ctx, "ETH", int64(1)).Return(big.NewInt(200000000000), nil)

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	w := newTestWallet(userID, walletAddress, 1)
	transfer := newTestTransfer(externalAddress, walletAddress, 1000000000000000000, sync.DirectionIn)

	err := processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)

	// Verify transfer_in was recorded
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferIn, ledgerSvc.recordedTransactions[0].TxType)
}

func TestProcessor_ClassifyTransfer_Outgoing(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddress := "0x1111111111111111111111111111111111111111"
	externalAddress := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	// External address is not a user wallet (user-scoped)
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddress, userID).Return([]*wallet.Wallet{}, nil)

	// Mock ledger to record outgoing transfer
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferOut, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Mock asset price
	assetSvc.On("GetPriceBySymbol", ctx, "ETH", int64(1)).Return(big.NewInt(200000000000), nil)

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	w := newTestWallet(userID, walletAddress, 1)
	transfer := newTestTransfer(walletAddress, externalAddress, 1000000000000000000, sync.DirectionOut)

	err := processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)

	// Verify transfer_out was recorded
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferOut, ledgerSvc.recordedTransactions[0].TxType)
}

func TestProcessor_ClassifyTransfer_Internal_FromOutgoing(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	sourceAddress := "0x1111111111111111111111111111111111111111"
	destAddress := "0x2222222222222222222222222222222222222222"
	destWalletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	// Dest address IS a user wallet (user-scoped)
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, destAddress, userID).Return([]*wallet.Wallet{
		{ID: destWalletID, UserID: userID, Address: destAddress, ChainID: 1},
	}, nil)

	// Mock ledger to record internal transfer
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeInternalTransfer, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Mock asset price
	assetSvc.On("GetPriceBySymbol", ctx, "ETH", int64(1)).Return(big.NewInt(200000000000), nil)

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	sourceWallet := newTestWallet(userID, sourceAddress, 1)
	transfer := newTestTransfer(sourceAddress, destAddress, 1000000000000000000, sync.DirectionOut)

	err := processor.ProcessTransfer(ctx, sourceWallet, transfer)
	require.NoError(t, err)

	// Verify internal_transfer was recorded (only from outgoing side)
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeInternalTransfer, ledgerSvc.recordedTransactions[0].TxType)

	// Verify source_wallet_id and dest_wallet_id are set
	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, sourceWallet.ID.String(), rawData["source_wallet_id"])
	assert.Equal(t, destWalletID.String(), rawData["dest_wallet_id"])
}

func TestProcessor_ClassifyTransfer_Internal_FromIncoming_Skipped(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	sourceAddress := "0x1111111111111111111111111111111111111111"
	destAddress := "0x2222222222222222222222222222222222222222"
	sourceWalletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	// Source address IS a user wallet (internal transfer, user-scoped)
	walletRepo.On("GetWalletsByAddressAndUserID", ctx, sourceAddress, userID).Return([]*wallet.Wallet{
		{ID: sourceWalletID, UserID: userID, Address: sourceAddress, ChainID: 1},
	}, nil)

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	destWallet := newTestWallet(userID, destAddress, 1)
	transfer := newTestTransfer(sourceAddress, destAddress, 1000000000000000000, sync.DirectionIn)

	err := processor.ProcessTransfer(ctx, destWallet, transfer)
	require.NoError(t, err)

	// Verify NO transaction was recorded (incoming side of internal is skipped)
	assert.Len(t, ledgerSvc.recordedTransactions, 0, "Internal transfer should be skipped on incoming side")
}

// =============================================================================
// USD Rate Tests
// =============================================================================

func TestProcessor_USDRate_Available(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddress := "0x1111111111111111111111111111111111111111"
	externalAddress := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddress, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Return a specific price
	ethPrice := big.NewInt(250000000000) // $2500
	assetSvc.On("GetPriceBySymbol", ctx, "ETH", int64(1)).Return(ethPrice, nil)

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	w := newTestWallet(userID, walletAddress, 1)
	transfer := newTestTransfer(externalAddress, walletAddress, 1000000000000000000, sync.DirectionIn)

	err := processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)

	// Verify USD rate was set
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, ethPrice.String(), rawData["usd_rate"])
}

func TestProcessor_USDRate_GracefulDegradation_NilPrice(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddress := "0x1111111111111111111111111111111111111111"
	externalAddress := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddress, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Return nil price (graceful degradation)
	assetSvc.On("GetPriceBySymbol", ctx, "ETH", int64(1)).Return(nil, nil)

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	w := newTestWallet(userID, walletAddress, 1)
	transfer := newTestTransfer(externalAddress, walletAddress, 1000000000000000000, sync.DirectionIn)

	err := processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)

	// Verify USD rate is "0" (graceful degradation)
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "0", rawData["usd_rate"])
}

func TestProcessor_USDRate_GracefulDegradation_Error(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddress := "0x1111111111111111111111111111111111111111"
	externalAddress := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddress, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// Return error (graceful degradation)
	assetSvc.On("GetPriceBySymbol", ctx, "ETH", int64(1)).Return(nil, assert.AnError)

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	w := newTestWallet(userID, walletAddress, 1)
	transfer := newTestTransfer(externalAddress, walletAddress, 1000000000000000000, sync.DirectionIn)

	err := processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)

	// Verify USD rate is "0" even with error
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "0", rawData["usd_rate"])
}

func TestProcessor_USDRate_GracefulDegradation_NoAssetService(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddress := "0x1111111111111111111111111111111111111111"
	externalAddress := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddress, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	// No asset service (nil)
	processor := newTestProcessor(walletRepo, ledgerSvc, nil)
	w := newTestWallet(userID, walletAddress, 1)
	transfer := newTestTransfer(externalAddress, walletAddress, 1000000000000000000, sync.DirectionIn)

	err := processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)

	// Verify USD rate is "0" with nil asset service
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData
	assert.Equal(t, "0", rawData["usd_rate"])
}

// =============================================================================
// Idempotency Tests
// =============================================================================

func TestProcessor_Idempotency_DuplicateIgnored(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddress := "0x1111111111111111111111111111111111111111"
	externalAddress := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddress, userID).Return([]*wallet.Wallet{}, nil)
	assetSvc.On("GetPriceBySymbol", ctx, "ETH", int64(1)).Return(big.NewInt(200000000000), nil)

	// First call succeeds
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	// Second call returns PostgreSQL unique constraint violation (code 23505)
	duplicateError := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, duplicateError).Once()

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	w := newTestWallet(userID, walletAddress, 1)
	transfer := newTestTransfer(externalAddress, walletAddress, 1000000000000000000, sync.DirectionIn)

	// First processing
	err := processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)
	assert.Len(t, ledgerSvc.recordedTransactions, 1)

	// Second processing (duplicate) — should be silently ignored
	err = processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)
	assert.Len(t, ledgerSvc.recordedTransactions, 2) // recorded in mock but error was handled
}

// =============================================================================
// Transfer Data Tests
// =============================================================================

func TestProcessor_TransferData_AllFieldsPopulated(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	walletAddress := "0x1111111111111111111111111111111111111111"
	externalAddress := "0x9999999999999999999999999999999999999999"

	walletRepo := new(MockWalletRepository)
	ledgerSvc := new(MockLedgerService)
	assetSvc := new(MockAssetService)

	walletRepo.On("GetWalletsByAddressAndUserID", ctx, externalAddress, userID).Return([]*wallet.Wallet{}, nil)
	ledgerSvc.On("RecordTransaction", ctx, ledger.TxTypeTransferIn, "blockchain", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)
	assetSvc.On("GetPriceBySymbol", ctx, "USDC", int64(1)).Return(big.NewInt(100000000), nil)

	processor := newTestProcessor(walletRepo, ledgerSvc, assetSvc)
	w := newTestWallet(userID, walletAddress, 1)

	transfer := sync.Transfer{
		TxHash:          "0xabc123def456",
		BlockNumber:     12345678,
		Timestamp:       time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		From:            externalAddress,
		To:              walletAddress,
		Amount:          big.NewInt(1000000000), // 1000 USDC (6 decimals)
		AssetSymbol:     "USDC",
		ContractAddress: "0xcontract123",
		Decimals:        6,
		ChainID:         1,
		Direction:       sync.DirectionIn,
		TransferType:    sync.TransferTypeERC20,
		UniqueID:        "unique-usdc-transfer",
	}

	err := processor.ProcessTransfer(ctx, w, transfer)
	require.NoError(t, err)

	// Verify all fields are populated correctly
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rawData := ledgerSvc.recordedTransactions[0].RawData

	assert.Equal(t, w.ID.String(), rawData["wallet_id"])
	assert.Equal(t, "USDC", rawData["asset_id"])
	assert.Equal(t, 6, rawData["decimals"])
	assert.Equal(t, transfer.Amount.String(), rawData["amount"])
	assert.Equal(t, int64(1), rawData["chain_id"])
	assert.Equal(t, "0xabc123def456", rawData["tx_hash"])
	assert.Equal(t, int64(12345678), rawData["block_number"])
	assert.Equal(t, externalAddress, rawData["from_address"])
	assert.Equal(t, "0xcontract123", rawData["contract_address"])
	assert.Equal(t, "unique-usdc-transfer", rawData["unique_id"])
}
