//go:build integration

package sync_test

import (
	"context"
	"log/slog"
	"math/big"
	"os"
	gosync "sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/testutil/testdb"
)

var testDB *testdb.TestDB

func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	testDB, err = testdb.NewTestDB(ctx)
	if err != nil {
		panic("failed to create test database: " + err.Error())
	}

	code := m.Run()

	testDB.Close(ctx)
	if code != 0 {
		panic("tests failed")
	}
}

// =============================================================================
// Test Helpers
// =============================================================================

type testEnv struct {
	syncSvc          *sync.Service
	ledgerSvc        *ledger.Service
	ledgerRepo       *postgres.LedgerRepository
	blockchainClient *mockBlockchainClient
	ctx              context.Context
}

func setupIntegrationTest(t *testing.T) *testEnv {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create repositories
	ledgerRepo := postgres.NewLedgerRepository(testDB.Pool)
	walletRepo := postgres.NewWalletRepository(testDB.Pool)

	// Create ledger service with transfer handlers
	registry := ledger.NewRegistry()
	registry.Register(transfer.NewTransferInHandler(walletRepo))
	registry.Register(transfer.NewTransferOutHandler(walletRepo))
	registry.Register(transfer.NewInternalTransferHandler(walletRepo))
	ledgerSvc := ledger.NewService(ledgerRepo, registry)

	// Create mock blockchain client
	blockchainClient := newMockBlockchainClient()

	// Create sync config
	config := &sync.Config{
		Enabled:                  true,
		PollInterval:             5 * time.Second,
		ConcurrentWallets:        3,
		InitialSyncBlockLookback: 100,
		MaxBlocksPerSync:         1000,
	}

	// Create sync service
	syncSvc := sync.NewService(config, blockchainClient, walletRepo, ledgerSvc, nil, logger)

	return &testEnv{
		syncSvc:          syncSvc,
		ledgerSvc:        ledgerSvc,
		ledgerRepo:       ledgerRepo,
		blockchainClient: blockchainClient,
		ctx:              ctx,
	}
}

// Helper to create a test user
func createTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	userID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
	`, userID, "test-"+userID.String()[:8]+"@example.com", "hash")
	require.NoError(t, err)
	return userID
}

// Helper to create a test wallet with sync fields
func createTestWallet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, address string, chainID int64) uuid.UUID {
	walletID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, chain_id, address, sync_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', NOW(), NOW())
	`, walletID, userID, "Test Wallet "+walletID.String()[:8], chainID, address)
	require.NoError(t, err)
	return walletID
}

// =============================================================================
// Mock Blockchain Client
// =============================================================================

type mockBlockchainClient struct {
	mu        gosync.Mutex
	transfers map[string][]sync.Transfer // address -> transfers
	blocks    map[int64]int64            // chainID -> currentBlock
}

func newMockBlockchainClient() *mockBlockchainClient {
	return &mockBlockchainClient{
		transfers: make(map[string][]sync.Transfer),
		blocks:    make(map[int64]int64),
	}
}

func (m *mockBlockchainClient) GetCurrentBlock(ctx context.Context, chainID int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if block, ok := m.blocks[chainID]; ok {
		return block, nil
	}
	return 12345678, nil // Default block
}

func (m *mockBlockchainClient) GetTransfers(ctx context.Context, chainID int64, address string, fromBlock, toBlock int64) ([]sync.Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := address
	if transfers, ok := m.transfers[key]; ok {
		// Filter by block range
		var result []sync.Transfer
		for _, t := range transfers {
			if t.BlockNumber >= fromBlock && t.BlockNumber <= toBlock && t.ChainID == chainID {
				result = append(result, t)
			}
		}
		return result, nil
	}
	return nil, nil
}

func (m *mockBlockchainClient) GetNativeAssetInfo(chainID int64) (string, int, error) {
	return "ETH", 18, nil
}

func (m *mockBlockchainClient) IsSupported(chainID int64) bool {
	return chainID == 1 // Only Ethereum mainnet
}

func (m *mockBlockchainClient) AddTransfer(address string, transfer sync.Transfer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transfers[address] = append(m.transfers[address], transfer)
}

func (m *mockBlockchainClient) SetCurrentBlock(chainID, block int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocks[chainID] = block
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestSyncService_SyncWallet_RecordsTransfers(t *testing.T) {
	env := setupIntegrationTest(t)

	// Create user and wallet
	userID := createTestUser(t, env.ctx, testDB.Pool)
	walletAddress := "0x1234567890123456789012345678901234567890"
	walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, walletAddress, 1)

	// Add mock transfers
	env.blockchainClient.SetCurrentBlock(1, 12345678)

	// Add incoming transfer
	env.blockchainClient.AddTransfer(walletAddress, sync.Transfer{
		TxHash:          "0xincoming123",
		BlockNumber:     12345600,
		Timestamp:       time.Now().Add(-time.Hour),
		From:            "0xexternalsender",
		To:              walletAddress,
		Amount:          big.NewInt(1000000000000000000), // 1 ETH
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		ChainID:         1,
		Direction:       sync.DirectionIn,
		TransferType:    sync.TransferTypeExternal,
		UniqueID:        "unique-incoming-1",
	})

	// Sync the wallet
	err := env.syncSvc.SyncWallet(env.ctx, walletID)
	require.NoError(t, err)

	// Verify transfer was recorded
	txs, err := env.ledgerSvc.ListTransactions(env.ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, txs, 1, "Should have 1 transaction recorded")

	// Verify balance
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := env.ledgerRepo.GetAccountByCode(env.ctx, accountCode)
	require.NoError(t, err)

	balance, err := env.ledgerSvc.GetAccountBalance(env.ctx, account.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(1000000000000000000)), "Balance should be 1 ETH")
}

func TestSyncService_SyncWallet_MultipleTransfers(t *testing.T) {
	env := setupIntegrationTest(t)

	userID := createTestUser(t, env.ctx, testDB.Pool)
	walletAddress := "0x1234567890123456789012345678901234567890"
	walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, walletAddress, 1)

	env.blockchainClient.SetCurrentBlock(1, 12345700)

	// Add multiple incoming transfers
	for i := 0; i < 5; i++ {
		env.blockchainClient.AddTransfer(walletAddress, sync.Transfer{
			TxHash:          "0xincoming" + string(rune('a'+i)),
			BlockNumber:     12345600 + int64(i*10),
			Timestamp:       time.Now().Add(-time.Duration(5-i) * time.Hour),
			From:            "0xexternalsender",
			To:              walletAddress,
			Amount:          big.NewInt(100000000000000000), // 0.1 ETH
			AssetSymbol:     "ETH",
			ContractAddress: "",
			Decimals:        18,
			ChainID:         1,
			Direction:       sync.DirectionIn,
			TransferType:    sync.TransferTypeExternal,
			UniqueID:        "unique-multi-" + string(rune('a'+i)),
		})
	}

	// Sync
	err := env.syncSvc.SyncWallet(env.ctx, walletID)
	require.NoError(t, err)

	// Verify all transfers were recorded
	txs, err := env.ledgerSvc.ListTransactions(env.ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, txs, 5, "Should have 5 transactions recorded")

	// Verify total balance: 5 * 0.1 = 0.5 ETH
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := env.ledgerRepo.GetAccountByCode(env.ctx, accountCode)
	require.NoError(t, err)

	balance, err := env.ledgerSvc.GetAccountBalance(env.ctx, account.ID, "ETH")
	require.NoError(t, err)
	expectedBalance := big.NewInt(500000000000000000) // 0.5 ETH
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance), "Balance should be 0.5 ETH")
}

func TestSyncService_InternalTransfer_RecordedOnce(t *testing.T) {
	env := setupIntegrationTest(t)

	userID := createTestUser(t, env.ctx, testDB.Pool)
	sourceAddress := "0x1111111111111111111111111111111111111111"
	destAddress := "0x2222222222222222222222222222222222222222"
	sourceWalletID := createTestWallet(t, env.ctx, testDB.Pool, userID, sourceAddress, 1)
	destWalletID := createTestWallet(t, env.ctx, testDB.Pool, userID, destAddress, 1)

	env.blockchainClient.SetCurrentBlock(1, 12345700)

	// Add outgoing transfer from source (will be classified as internal)
	env.blockchainClient.AddTransfer(sourceAddress, sync.Transfer{
		TxHash:          "0xinternal123",
		BlockNumber:     12345650,
		Timestamp:       time.Now().Add(-time.Hour),
		From:            sourceAddress,
		To:              destAddress,
		Amount:          big.NewInt(500000000000000000), // 0.5 ETH
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		ChainID:         1,
		Direction:       sync.DirectionOut,
		TransferType:    sync.TransferTypeExternal,
		UniqueID:        "unique-internal-out",
	})

	// Add incoming transfer to dest (same transaction, should be skipped)
	env.blockchainClient.AddTransfer(destAddress, sync.Transfer{
		TxHash:          "0xinternal123",
		BlockNumber:     12345650,
		Timestamp:       time.Now().Add(-time.Hour),
		From:            sourceAddress,
		To:              destAddress,
		Amount:          big.NewInt(500000000000000000), // 0.5 ETH
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		ChainID:         1,
		Direction:       sync.DirectionIn,
		TransferType:    sync.TransferTypeExternal,
		UniqueID:        "unique-internal-in",
	})

	// Sync both wallets
	err := env.syncSvc.SyncWallet(env.ctx, sourceWalletID)
	require.NoError(t, err)

	err = env.syncSvc.SyncWallet(env.ctx, destWalletID)
	require.NoError(t, err)

	// Verify only ONE internal_transfer transaction was recorded
	internalTransferType := string(ledger.TxTypeInternalTransfer)
	txs, err := env.ledgerSvc.ListTransactions(env.ctx, ledger.TransactionFilters{
		Type: &internalTransferType,
	})
	require.NoError(t, err)
	assert.Len(t, txs, 1, "Should have exactly 1 internal transfer (not duplicated)")

	// Verify balances
	// Source wallet should have decreased
	sourceAccountCode := "wallet." + sourceWalletID.String() + ".ETH"
	sourceAccount, err := env.ledgerRepo.GetAccountByCode(env.ctx, sourceAccountCode)
	require.NoError(t, err)

	sourceBalance, err := env.ledgerSvc.GetAccountBalance(env.ctx, sourceAccount.ID, "ETH")
	require.NoError(t, err)
	// Balance is negative because we didn't add initial balance
	expectedSourceBalance := big.NewInt(-500000000000000000)
	assert.Equal(t, 0, sourceBalance.Balance.Cmp(expectedSourceBalance))

	// Dest wallet should have increased
	destAccountCode := "wallet." + destWalletID.String() + ".ETH"
	destAccount, err := env.ledgerRepo.GetAccountByCode(env.ctx, destAccountCode)
	require.NoError(t, err)

	destBalance, err := env.ledgerSvc.GetAccountBalance(env.ctx, destAccount.ID, "ETH")
	require.NoError(t, err)
	expectedDestBalance := big.NewInt(500000000000000000)
	assert.Equal(t, 0, destBalance.Balance.Cmp(expectedDestBalance))
}

func TestSyncService_ConcurrentWalletSync_NoRace(t *testing.T) {
	env := setupIntegrationTest(t)

	userID := createTestUser(t, env.ctx, testDB.Pool)

	// Create multiple wallets
	var walletIDs []uuid.UUID
	for i := 0; i < 5; i++ {
		address := "0x" + string(rune('1'+i)) + "111111111111111111111111111111111111111"
		walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, address, 1)
		walletIDs = append(walletIDs, walletID)

		// Add transfer for each wallet
		env.blockchainClient.AddTransfer(address, sync.Transfer{
			TxHash:          "0xtx" + string(rune('a'+i)),
			BlockNumber:     12345600 + int64(i*10),
			Timestamp:       time.Now().Add(-time.Hour),
			From:            "0xexternalsender",
			To:              address,
			Amount:          big.NewInt(100000000000000000), // 0.1 ETH
			AssetSymbol:     "ETH",
			ContractAddress: "",
			Decimals:        18,
			ChainID:         1,
			Direction:       sync.DirectionIn,
			TransferType:    sync.TransferTypeExternal,
			UniqueID:        "unique-concurrent-" + string(rune('a'+i)),
		})
	}

	env.blockchainClient.SetCurrentBlock(1, 12345700)

	// Sync all wallets concurrently
	var wg gosync.WaitGroup
	errors := make(chan error, len(walletIDs))

	for _, walletID := range walletIDs {
		wg.Add(1)
		go func(wid uuid.UUID) {
			defer wg.Done()
			if err := env.syncSvc.SyncWallet(env.ctx, wid); err != nil {
				errors <- err
			}
		}(walletID)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent sync error: %v", err)
	}

	// Verify all transfers were recorded
	txs, err := env.ledgerSvc.ListTransactions(env.ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, txs, 5, "Should have 5 transactions recorded (one per wallet)")
}

func TestSyncService_Idempotency_DoubleSyncSameWallet(t *testing.T) {
	env := setupIntegrationTest(t)

	userID := createTestUser(t, env.ctx, testDB.Pool)
	walletAddress := "0x1234567890123456789012345678901234567890"
	walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, walletAddress, 1)

	env.blockchainClient.SetCurrentBlock(1, 12345678)

	// Add a transfer
	env.blockchainClient.AddTransfer(walletAddress, sync.Transfer{
		TxHash:          "0xidempotent123",
		BlockNumber:     12345600,
		Timestamp:       time.Now().Add(-time.Hour),
		From:            "0xexternalsender",
		To:              walletAddress,
		Amount:          big.NewInt(1000000000000000000), // 1 ETH
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		ChainID:         1,
		Direction:       sync.DirectionIn,
		TransferType:    sync.TransferTypeExternal,
		UniqueID:        "unique-idempotent",
	})

	// First sync
	err := env.syncSvc.SyncWallet(env.ctx, walletID)
	require.NoError(t, err)

	// Update wallet status back to pending for second sync
	_, err = testDB.Pool.Exec(env.ctx, `UPDATE wallets SET sync_status = 'pending' WHERE id = $1`, walletID)
	require.NoError(t, err)

	// Second sync (should be idempotent due to external_id)
	err = env.syncSvc.SyncWallet(env.ctx, walletID)
	require.NoError(t, err)

	// Verify only ONE transaction was recorded (due to external_id uniqueness)
	txs, err := env.ledgerSvc.ListTransactions(env.ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, txs, 1, "Should have exactly 1 transaction (not duplicated)")

	// Verify balance is correct (not doubled)
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := env.ledgerRepo.GetAccountByCode(env.ctx, accountCode)
	require.NoError(t, err)

	balance, err := env.ledgerSvc.GetAccountBalance(env.ctx, account.ID, "ETH")
	require.NoError(t, err)
	expectedBalance := big.NewInt(1000000000000000000) // 1 ETH
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance), "Balance should be 1 ETH (not doubled)")
}

func TestSyncService_MixedTransfers_InOutExternal(t *testing.T) {
	env := setupIntegrationTest(t)

	userID := createTestUser(t, env.ctx, testDB.Pool)
	walletAddress := "0x1234567890123456789012345678901234567890"
	walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, walletAddress, 1)

	env.blockchainClient.SetCurrentBlock(1, 12345700)

	// Add incoming transfer: +2 ETH
	env.blockchainClient.AddTransfer(walletAddress, sync.Transfer{
		TxHash:          "0xin1",
		BlockNumber:     12345600,
		Timestamp:       time.Now().Add(-3 * time.Hour),
		From:            "0xexternalsender",
		To:              walletAddress,
		Amount:          big.NewInt(2000000000000000000), // 2 ETH
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		ChainID:         1,
		Direction:       sync.DirectionIn,
		TransferType:    sync.TransferTypeExternal,
		UniqueID:        "unique-in-1",
	})

	// Add outgoing transfer: -0.5 ETH
	env.blockchainClient.AddTransfer(walletAddress, sync.Transfer{
		TxHash:          "0xout1",
		BlockNumber:     12345620,
		Timestamp:       time.Now().Add(-2 * time.Hour),
		From:            walletAddress,
		To:              "0xexternalreceiver",
		Amount:          big.NewInt(500000000000000000), // 0.5 ETH
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		ChainID:         1,
		Direction:       sync.DirectionOut,
		TransferType:    sync.TransferTypeExternal,
		UniqueID:        "unique-out-1",
	})

	// Add another incoming: +1 ETH
	env.blockchainClient.AddTransfer(walletAddress, sync.Transfer{
		TxHash:          "0xin2",
		BlockNumber:     12345640,
		Timestamp:       time.Now().Add(-time.Hour),
		From:            "0xexternalsender2",
		To:              walletAddress,
		Amount:          big.NewInt(1000000000000000000), // 1 ETH
		AssetSymbol:     "ETH",
		ContractAddress: "",
		Decimals:        18,
		ChainID:         1,
		Direction:       sync.DirectionIn,
		TransferType:    sync.TransferTypeExternal,
		UniqueID:        "unique-in-2",
	})

	// Sync
	err := env.syncSvc.SyncWallet(env.ctx, walletID)
	require.NoError(t, err)

	// Verify 3 transactions recorded
	txs, err := env.ledgerSvc.ListTransactions(env.ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, txs, 3)

	// Verify final balance: 2 - 0.5 + 1 = 2.5 ETH
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := env.ledgerRepo.GetAccountByCode(env.ctx, accountCode)
	require.NoError(t, err)

	balance, err := env.ledgerSvc.GetAccountBalance(env.ctx, account.ID, "ETH")
	require.NoError(t, err)
	expectedBalance := big.NewInt(2500000000000000000) // 2.5 ETH
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance), "Balance should be 2.5 ETH")
}
