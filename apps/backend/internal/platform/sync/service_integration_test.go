//go:build integration

package sync_test

import (
	"context"
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
	"github.com/kislikjeka/moontrack/pkg/logger"
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
	syncSvc        *sync.Service
	ledgerSvc      *ledger.Service
	ledgerRepo     *postgres.LedgerRepository
	txProviderMock *mockTxProvider
	ctx            context.Context
}

func setupIntegrationTest(t *testing.T) *testEnv {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	log := logger.New("test", os.Stdout)

	// Create repositories
	ledgerRepo := postgres.NewLedgerRepository(testDB.Pool)
	walletRepo := postgres.NewWalletRepository(testDB.Pool)

	// Create ledger service with transfer handlers
	registry := ledger.NewRegistry()
	registry.Register(transfer.NewTransferInHandler(walletRepo, log))
	registry.Register(transfer.NewTransferOutHandler(walletRepo, log))
	registry.Register(transfer.NewInternalTransferHandler(walletRepo, log))
	ledgerSvc := ledger.NewService(ledgerRepo, registry, log)

	// Create mock transaction data provider
	txProviderMock := newMockTxProvider()

	// Sync is a two-phase pipeline since #26: the collector durably stages every
	// fetched transaction in raw_transactions and the processor drains that
	// store. NewService only builds those sub-services when the raw store and
	// asset repo are present, so passing nil here would leave svc.collector nil
	// and SyncWallet would nil-panic rather than sync anything.
	rawTxRepo := postgres.NewRawTransactionRepository(testDB.Pool)
	syncAssetRepo := postgres.NewSyncAssetRepository(testDB.Pool)

	// Create sync config
	config := &sync.Config{
		Enabled:             true,
		PollInterval:        5 * time.Second,
		ConcurrentWallets:   3,
		InitialSyncLookback: 0,
	}

	// Create sync service
	syncSvc := sync.NewService(config, walletRepo, ledgerSvc, nil, log, txProviderMock, nil, rawTxRepo, syncAssetRepo, nil, nil, nil, nil, nil, nil)

	return &testEnv{
		syncSvc:        syncSvc,
		ledgerSvc:      ledgerSvc,
		ledgerRepo:     ledgerRepo,
		txProviderMock: txProviderMock,
		ctx:            ctx,
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

// Helper to create a test wallet with sync fields. Seeds a single "ethereum"
// wallet_chain_sync row so the collector/reconciler fan-out (issue #27) iterates
// exactly the chain the ethereum-only fixtures live on.
func createTestWallet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, address string) uuid.UUID {
	walletID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, address, sync_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', NOW(), NOW())
	`, walletID, userID, "Test Wallet "+walletID.String()[:8], address)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO wallet_chain_sync (wallet_id, chain, sync_status, sync_phase)
		VALUES ($1, 'ethereum', 'pending', 'idle')
	`, walletID)
	require.NoError(t, err)

	return walletID
}

// =============================================================================
// Mock Transaction Data Provider
// =============================================================================

type mockTxProvider struct {
	mu           gosync.Mutex
	transactions map[string][]sync.DecodedTransaction // address -> transactions
}

func newMockTxProvider() *mockTxProvider {
	return &mockTxProvider{
		transactions: make(map[string][]sync.DecodedTransaction),
	}
}

func (m *mockTxProvider) GetTransactions(ctx context.Context, address, chain string, since time.Time) ([]sync.DecodedTransaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// The collector fans out over chains, invoking this once per chain. Return only
	// the transactions that belong to the requested chain so each tx is fetched once.
	if txs, ok := m.transactions[address]; ok {
		var result []sync.DecodedTransaction
		for _, tx := range txs {
			if tx.ChainID != chain {
				continue
			}
			if !tx.MinedAt.Before(since) {
				result = append(result, tx)
			}
		}
		return result, nil
	}
	return nil, nil
}

// StreamTransactions satisfies the paging half of TransactionDataProvider
// (added in #29). The fake holds everything in memory, so the whole chain's
// result is one page — the collector's per-page bookkeeping is exercised the
// same way, just with a single invocation of onPage.
func (m *mockTxProvider) StreamTransactions(
	ctx context.Context,
	address, chain string,
	since time.Time,
	onPage func([]sync.DecodedTransaction) error,
) error {
	page, err := m.GetTransactions(ctx, address, chain, since)
	if err != nil {
		return err
	}
	if len(page) == 0 {
		return nil
	}
	return onPage(page)
}

func (m *mockTxProvider) AddTransaction(address string, tx sync.DecodedTransaction) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transactions[address] = append(m.transactions[address], tx)
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestSyncService_SyncWallet_RecordsTransfers(t *testing.T) {
	env := setupIntegrationTest(t)

	// Create user and wallet
	userID := createTestUser(t, env.ctx, testDB.Pool)
	walletAddress := "0x1234567890123456789012345678901234567890"
	walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, walletAddress)

	// Add incoming transfer via provider mock
	env.txProviderMock.AddTransaction(walletAddress, sync.DecodedTransaction{
		ID:            "ext-tx-1",
		TxHash:        "0xincoming123",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(1000000000000000000), // 1 ETH
				Direction:   sync.DirectionIn,
				Sender:      "0xexternalsender",
				Recipient:   walletAddress,
				USDPrice:    big.NewInt(250000000000),
			},
		},
		MinedAt: time.Now().Add(-time.Hour),
		Status:  "confirmed",
	})

	// Sync the wallet
	err := env.syncSvc.SyncWallet(env.ctx, walletID)
	require.NoError(t, err)

	// Verify transfer was recorded
	txs, err := env.ledgerSvc.ListTransactions(env.ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, txs, 1, "Should have 1 transaction recorded")

	// Verify balance
	accountCode := "wallet." + walletID.String() + ".ethereum.ETH"
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
	walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, walletAddress)

	// Add multiple incoming transfers
	for i := 0; i < 5; i++ {
		env.txProviderMock.AddTransaction(walletAddress, sync.DecodedTransaction{
			ID:            "ext-multi-" + string(rune('a'+i)),
			TxHash:        "0xincoming" + string(rune('a'+i)),
			ChainID:       "ethereum",
			OperationType: sync.OpReceive,
			Transfers: []sync.DecodedTransfer{
				{
					AssetSymbol: "ETH",
					Decimals:    18,
					Amount:      big.NewInt(100000000000000000), // 0.1 ETH
					Direction:   sync.DirectionIn,
					Sender:      "0xexternalsender",
					Recipient:   walletAddress,
					USDPrice:    big.NewInt(250000000000),
				},
			},
			MinedAt: time.Now().Add(-time.Duration(5-i) * time.Hour),
			Status:  "confirmed",
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
	accountCode := "wallet." + walletID.String() + ".ethereum.ETH"
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
	sourceWalletID := createTestWallet(t, env.ctx, testDB.Pool, userID, sourceAddress)
	destWalletID := createTestWallet(t, env.ctx, testDB.Pool, userID, destAddress)

	// Fund the source wallet first. The ledger refuses to drive an account
	// negative, so a wallet that sends 0.5 ETH it never received is rejected
	// outright — the internal transfer would never be recorded and this test
	// would be asserting on an empty ledger.
	env.txProviderMock.AddTransaction(sourceAddress, sync.DecodedTransaction{
		ID:            "ext-internal-funding",
		TxHash:        "0xfunding123",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(1000000000000000000), // 1 ETH
				Direction:   sync.DirectionIn,
				Sender:      "0xexternalsender",
				Recipient:   sourceAddress,
				USDPrice:    big.NewInt(250000000000),
			},
		},
		MinedAt: time.Now().Add(-2 * time.Hour),
		Status:  "confirmed",
	})

	// Add outgoing transfer from source (will be classified as internal)
	env.txProviderMock.AddTransaction(sourceAddress, sync.DecodedTransaction{
		ID:            "ext-internal-out",
		TxHash:        "0xinternal123",
		ChainID:       "ethereum",
		OperationType: sync.OpSend,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(500000000000000000), // 0.5 ETH
				Direction:   sync.DirectionOut,
				Sender:      sourceAddress,
				Recipient:   destAddress,
				USDPrice:    big.NewInt(250000000000),
			},
		},
		MinedAt: time.Now().Add(-time.Hour),
		Status:  "confirmed",
	})

	// Add incoming transfer to dest (same transaction, should be skipped)
	env.txProviderMock.AddTransaction(destAddress, sync.DecodedTransaction{
		ID:            "ext-internal-in",
		TxHash:        "0xinternal123",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(500000000000000000), // 0.5 ETH
				Direction:   sync.DirectionIn,
				Sender:      sourceAddress,
				Recipient:   destAddress,
				USDPrice:    big.NewInt(250000000000),
			},
		},
		MinedAt: time.Now().Add(-time.Hour),
		Status:  "confirmed",
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
	// Source wallet: funded with 1 ETH, sent 0.5 away.
	sourceAccountCode := "wallet." + sourceWalletID.String() + ".ethereum.ETH"
	sourceAccount, err := env.ledgerRepo.GetAccountByCode(env.ctx, sourceAccountCode)
	require.NoError(t, err)

	sourceBalance, err := env.ledgerSvc.GetAccountBalance(env.ctx, sourceAccount.ID, "ETH")
	require.NoError(t, err)
	expectedSourceBalance := big.NewInt(500000000000000000)
	assert.Equal(t, 0, sourceBalance.Balance.Cmp(expectedSourceBalance))

	// Dest wallet should have increased
	destAccountCode := "wallet." + destWalletID.String() + ".ethereum.ETH"
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
		walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, address)
		walletIDs = append(walletIDs, walletID)

		// Add transfer for each wallet
		env.txProviderMock.AddTransaction(address, sync.DecodedTransaction{
			ID:            "ext-concurrent-" + string(rune('a'+i)),
			TxHash:        "0xtx" + string(rune('a'+i)),
			ChainID:       "ethereum",
			OperationType: sync.OpReceive,
			Transfers: []sync.DecodedTransfer{
				{
					AssetSymbol: "ETH",
					Decimals:    18,
					Amount:      big.NewInt(100000000000000000), // 0.1 ETH
					Direction:   sync.DirectionIn,
					Sender:      "0xexternalsender",
					Recipient:   address,
					USDPrice:    big.NewInt(250000000000),
				},
			},
			MinedAt: time.Now().Add(-time.Hour),
			Status:  "confirmed",
		})
	}

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
	walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, walletAddress)

	// Add a transfer
	env.txProviderMock.AddTransaction(walletAddress, sync.DecodedTransaction{
		ID:            "ext-idempotent",
		TxHash:        "0xidempotent123",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(1000000000000000000), // 1 ETH
				Direction:   sync.DirectionIn,
				Sender:      "0xexternalsender",
				Recipient:   walletAddress,
				USDPrice:    big.NewInt(250000000000),
			},
		},
		MinedAt: time.Now().Add(-time.Hour),
		Status:  "confirmed",
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
	accountCode := "wallet." + walletID.String() + ".ethereum.ETH"
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
	walletID := createTestWallet(t, env.ctx, testDB.Pool, userID, walletAddress)

	// Add incoming transfer: +2 ETH
	env.txProviderMock.AddTransaction(walletAddress, sync.DecodedTransaction{
		ID:            "ext-in-1",
		TxHash:        "0xin1",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(2000000000000000000), // 2 ETH
				Direction:   sync.DirectionIn,
				Sender:      "0xexternalsender",
				Recipient:   walletAddress,
				USDPrice:    big.NewInt(250000000000),
			},
		},
		MinedAt: time.Now().Add(-3 * time.Hour),
		Status:  "confirmed",
	})

	// Add outgoing transfer: -0.5 ETH
	env.txProviderMock.AddTransaction(walletAddress, sync.DecodedTransaction{
		ID:            "ext-out-1",
		TxHash:        "0xout1",
		ChainID:       "ethereum",
		OperationType: sync.OpSend,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(500000000000000000), // 0.5 ETH
				Direction:   sync.DirectionOut,
				Sender:      walletAddress,
				Recipient:   "0xexternalreceiver",
				USDPrice:    big.NewInt(250000000000),
			},
		},
		MinedAt: time.Now().Add(-2 * time.Hour),
		Status:  "confirmed",
	})

	// Add another incoming: +1 ETH
	env.txProviderMock.AddTransaction(walletAddress, sync.DecodedTransaction{
		ID:            "ext-in-2",
		TxHash:        "0xin2",
		ChainID:       "ethereum",
		OperationType: sync.OpReceive,
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol: "ETH",
				Decimals:    18,
				Amount:      big.NewInt(1000000000000000000), // 1 ETH
				Direction:   sync.DirectionIn,
				Sender:      "0xexternalsender2",
				Recipient:   walletAddress,
				USDPrice:    big.NewInt(250000000000),
			},
		},
		MinedAt: time.Now().Add(-time.Hour),
		Status:  "confirmed",
	})

	// Sync
	err := env.syncSvc.SyncWallet(env.ctx, walletID)
	require.NoError(t, err)

	// Verify 3 transactions recorded
	txs, err := env.ledgerSvc.ListTransactions(env.ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, txs, 3)

	// Verify final balance: 2 - 0.5 + 1 = 2.5 ETH
	accountCode := "wallet." + walletID.String() + ".ethereum.ETH"
	account, err := env.ledgerRepo.GetAccountByCode(env.ctx, accountCode)
	require.NoError(t, err)

	balance, err := env.ledgerSvc.GetAccountBalance(env.ctx, account.ID, "ETH")
	require.NoError(t, err)
	expectedBalance := big.NewInt(2500000000000000000) // 2.5 ETH
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance), "Balance should be 2.5 ETH")
}
