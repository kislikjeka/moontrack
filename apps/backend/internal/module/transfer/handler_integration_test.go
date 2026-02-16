//go:build integration

package transfer_test

import (
	"context"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
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

// setupTransferTest sets up the test environment for transfer handler integration tests
func setupTransferTest(t *testing.T) (*ledger.Service, *postgres.LedgerRepository, context.Context) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	walletRepo := postgres.NewWalletRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	// Register transfer handlers
	log := logger.New("test", io.Discard)
	registry.Register(transfer.NewTransferInHandler(walletRepo, log))
	registry.Register(transfer.NewTransferOutHandler(walletRepo, log))
	registry.Register(transfer.NewInternalTransferHandler(walletRepo, log))

	svc := ledger.NewService(repo, registry, logger.New("test", io.Discard))
	return svc, repo, ctx
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

// Helper to create a test wallet with blockchain fields
func createTestWallet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, address string, chainID int64) uuid.UUID {
	walletID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, chain_id, address, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
	`, walletID, userID, "Test Wallet "+walletID.String()[:8], chainID, address)
	require.NoError(t, err)
	return walletID
}

// =============================================================================
// TransferIn E2E Integration Tests
// =============================================================================

func TestTransferIn_E2E_CreatesBalancedEntries(t *testing.T) {
	svc, repo, ctx := setupTransferTest(t)

	// Create user and wallet
	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID, "0x1234567890123456789012345678901234567890", 1)

	// Record a transfer_in transaction
	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeTransferIn,
		"blockchain",
		stringPtr("unique-transfer-in-1"),
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),        // $2000
			"chain_id":         int64(1),
			"tx_hash":          "0xabc123def456",
			"block_number":     int64(12345678),
			"from_address":     "0xsender123",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-time.Hour).Format(time.RFC3339),
			"unique_id":        "unique-transfer-in-1",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Verify transaction status
	assert.Equal(t, ledger.TransactionStatusCompleted, tx.Status)
	assert.Equal(t, ledger.TxTypeTransferIn, tx.Type)

	// Verify entries are balanced (double-entry accounting)
	require.Len(t, tx.Entries, 2)
	err = tx.VerifyBalance()
	assert.NoError(t, err, "Entries must be balanced")

	// Verify entry types
	assert.Equal(t, ledger.Debit, tx.Entries[0].DebitCredit)
	assert.Equal(t, ledger.EntryTypeAssetIncrease, tx.Entries[0].EntryType)
	assert.Equal(t, ledger.Credit, tx.Entries[1].DebitCredit)
	assert.Equal(t, ledger.EntryTypeIncome, tx.Entries[1].EntryType)

	// Verify wallet balance increased
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(1000000000000000000)), "Balance should be 1 ETH")
}

func TestTransferIn_E2E_MultipleTransfers(t *testing.T) {
	svc, repo, ctx := setupTransferTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID, "0x1234567890123456789012345678901234567890", 1)

	// Record multiple transfers
	for i := 0; i < 3; i++ {
		_, err := svc.RecordTransaction(
			ctx,
			ledger.TxTypeTransferIn,
			"blockchain",
			stringPtr("unique-multi-" + string(rune('a'+i))),
			time.Now().Add(-time.Duration(i+1)*time.Hour),
			map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         "ETH",
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(100000000000000000).String(), // 0.1 ETH
				"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
				"chain_id":         int64(1),
				"tx_hash":          "0xtx" + string(rune('a'+i)),
				"block_number":     int64(12345678 + i),
				"from_address":     "0xsender",
				"contract_address": "",
				"occurred_at":      time.Now().Add(-time.Duration(i+1) * time.Hour).Format(time.RFC3339),
				"unique_id":        "unique-multi-" + string(rune('a'+i)),
			},
		)
		require.NoError(t, err)
	}

	// Verify accumulated balance: 0.1 * 3 = 0.3 ETH
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)
	expectedBalance := big.NewInt(300000000000000000) // 0.3 ETH
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance), "Balance should be 0.3 ETH")
}

// =============================================================================
// TransferOut E2E Integration Tests
// =============================================================================

func TestTransferOut_E2E_DecreasesBalance(t *testing.T) {
	svc, repo, ctx := setupTransferTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID, "0x1234567890123456789012345678901234567890", 1)

	// First, add some balance via transfer_in
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeTransferIn,
		"blockchain",
		stringPtr("initial-balance"),
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(2000000000000000000).String(), // 2 ETH
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         int64(1),
			"tx_hash":          "0xincoming",
			"block_number":     int64(12345000),
			"from_address":     "0xfaucet",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			"unique_id":        "initial-balance",
		},
	)
	require.NoError(t, err)

	// Now record a transfer_out transaction
	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeTransferOut,
		"blockchain",
		stringPtr("outgoing-transfer-1"),
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(500000000000000000).String(), // 0.5 ETH
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         int64(1),
			"tx_hash":          "0xoutgoing123",
			"block_number":     int64(12345100),
			"to_address":       "0xreceiver",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-time.Hour).Format(time.RFC3339),
			"unique_id":        "outgoing-transfer-1",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Verify entries are balanced
	assert.Equal(t, ledger.TxTypeTransferOut, tx.Type)
	require.Len(t, tx.Entries, 2)
	err = tx.VerifyBalance()
	assert.NoError(t, err, "Entries must be balanced")

	// Verify wallet balance decreased: 2 - 0.5 = 1.5 ETH
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)
	expectedBalance := big.NewInt(1500000000000000000) // 1.5 ETH
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance), "Balance should be 1.5 ETH after outgoing transfer")
}

func TestTransferOut_E2E_WithGas(t *testing.T) {
	svc, _, ctx := setupTransferTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID, "0x1234567890123456789012345678901234567890", 1)

	// First, add some balance
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeTransferIn,
		"blockchain",
		stringPtr("initial-with-gas"),
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(5000000000000000000).String(), // 5 ETH
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         int64(1),
			"tx_hash":          "0xinitial",
			"block_number":     int64(12345000),
			"from_address":     "0xfaucet",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			"unique_id":        "initial-with-gas",
		},
	)
	require.NoError(t, err)

	// Record transfer out with gas
	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeTransferOut,
		"blockchain",
		stringPtr("outgoing-with-gas"),
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"gas_amount":       money.NewBigIntFromInt64(21000000000000000).String(), // 0.021 ETH gas
			"gas_usd_rate":     money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         int64(1),
			"tx_hash":          "0xwithgas",
			"block_number":     int64(12345100),
			"to_address":       "0xreceiver",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-time.Hour).Format(time.RFC3339),
			"unique_id":        "outgoing-with-gas",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// With gas, should have 4 entries
	require.Len(t, tx.Entries, 4, "Transfer out with gas should have 4 entries")
	err = tx.VerifyBalance()
	assert.NoError(t, err, "All entries must be balanced")
}

// =============================================================================
// InternalTransfer E2E Integration Tests
// =============================================================================

func TestInternalTransfer_E2E_MovesBalance(t *testing.T) {
	svc, repo, ctx := setupTransferTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	sourceWalletID := createTestWallet(t, ctx, testDB.Pool, userID, "0x1111111111111111111111111111111111111111", 1)
	destWalletID := createTestWallet(t, ctx, testDB.Pool, userID, "0x2222222222222222222222222222222222222222", 1)

	// Add initial balance to source wallet
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeTransferIn,
		"blockchain",
		stringPtr("source-initial"),
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":        sourceWalletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(3000000000000000000).String(), // 3 ETH
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         int64(1),
			"tx_hash":          "0xinitial",
			"block_number":     int64(12345000),
			"from_address":     "0xfaucet",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
			"unique_id":        "source-initial",
		},
	)
	require.NoError(t, err)

	// Record internal transfer
	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeInternalTransfer,
		"blockchain",
		stringPtr("internal-transfer-1"),
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"source_wallet_id": sourceWalletID.String(),
			"dest_wallet_id":   destWalletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(1000000000000000000).String(), // 1 ETH
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         int64(1),
			"tx_hash":          "0xinternal123",
			"block_number":     int64(12345100),
			"contract_address": "",
			"occurred_at":      time.Now().Add(-time.Hour).Format(time.RFC3339),
			"unique_id":        "internal-transfer-1",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Verify entries are balanced
	assert.Equal(t, ledger.TxTypeInternalTransfer, tx.Type)
	require.Len(t, tx.Entries, 2)
	err = tx.VerifyBalance()
	assert.NoError(t, err, "Internal transfer entries must be balanced")

	// Verify source wallet balance decreased: 3 - 1 = 2 ETH
	sourceAccountCode := "wallet." + sourceWalletID.String() + ".ETH"
	sourceAccount, err := repo.GetAccountByCode(ctx, sourceAccountCode)
	require.NoError(t, err)

	sourceBalance, err := svc.GetAccountBalance(ctx, sourceAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, sourceBalance.Balance.Cmp(big.NewInt(2000000000000000000)), "Source wallet should have 2 ETH")

	// Verify destination wallet balance increased: 0 + 1 = 1 ETH
	destAccountCode := "wallet." + destWalletID.String() + ".ETH"
	destAccount, err := repo.GetAccountByCode(ctx, destAccountCode)
	require.NoError(t, err)

	destBalance, err := svc.GetAccountBalance(ctx, destAccount.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, 0, destBalance.Balance.Cmp(big.NewInt(1000000000000000000)), "Dest wallet should have 1 ETH")
}

// =============================================================================
// Reconciliation Tests
// =============================================================================

func TestTransfer_Reconciliation_AfterMultipleTransfers(t *testing.T) {
	svc, repo, ctx := setupTransferTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID, "0x1234567890123456789012345678901234567890", 1)

	// Record multiple incoming transfers
	for i := 0; i < 5; i++ {
		_, err := svc.RecordTransaction(
			ctx,
			ledger.TxTypeTransferIn,
			"blockchain",
			stringPtr("recon-in-" + string(rune('0'+i))),
			time.Now().Add(-time.Duration(10-i)*time.Hour),
			map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         "ETH",
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(100000000000000000).String(), // 0.1 ETH
				"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
				"chain_id":         int64(1),
				"tx_hash":          "0xin" + string(rune('0'+i)),
				"block_number":     int64(12345000 + i),
				"from_address":     "0xsender",
				"contract_address": "",
				"occurred_at":      time.Now().Add(-time.Duration(10-i) * time.Hour).Format(time.RFC3339),
				"unique_id":        "recon-in-" + string(rune('0'+i)),
			},
		)
		require.NoError(t, err)
	}

	// Record some outgoing transfers
	for i := 0; i < 2; i++ {
		_, err := svc.RecordTransaction(
			ctx,
			ledger.TxTypeTransferOut,
			"blockchain",
			stringPtr("recon-out-" + string(rune('0'+i))),
			time.Now().Add(-time.Duration(5-i)*time.Hour),
			map[string]interface{}{
				"wallet_id":        walletID.String(),
				"asset_id":         "ETH",
				"decimals":         18,
				"amount":           money.NewBigIntFromInt64(50000000000000000).String(), // 0.05 ETH
				"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
				"chain_id":         int64(1),
				"tx_hash":          "0xout" + string(rune('0'+i)),
				"block_number":     int64(12345100 + i),
				"to_address":       "0xreceiver",
				"contract_address": "",
				"occurred_at":      time.Now().Add(-time.Duration(5-i) * time.Hour).Format(time.RFC3339),
				"unique_id":        "recon-out-" + string(rune('0'+i)),
			},
		)
		require.NoError(t, err)
	}

	// Get wallet account and reconcile
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	// Reconcile should pass
	err = svc.ReconcileBalance(ctx, account.ID, "ETH")
	assert.NoError(t, err, "Reconciliation should pass after multiple transfers")

	// Verify final balance: 5 * 0.1 - 2 * 0.05 = 0.5 - 0.1 = 0.4 ETH
	balance, err := svc.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)
	expectedBalance := big.NewInt(400000000000000000) // 0.4 ETH
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance), "Balance should be 0.4 ETH")
}

// =============================================================================
// Idempotency Tests
// =============================================================================

func TestTransferIn_Idempotency_NoDuplicates(t *testing.T) {
	svc, repo, ctx := setupTransferTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID, "0x1234567890123456789012345678901234567890", 1)

	externalID := "idempotent-transfer-123"

	// Record the first transfer
	tx1, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeTransferIn,
		"blockchain",
		&externalID,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         int64(1),
			"tx_hash":          "0xidempotent",
			"block_number":     int64(12345678),
			"from_address":     "0xsender",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-time.Hour).Format(time.RFC3339),
			"unique_id":        externalID,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx1)

	// Try to record the same transfer again (same external_id)
	tx2, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeTransferIn,
		"blockchain",
		&externalID,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":        walletID.String(),
			"asset_id":         "ETH",
			"decimals":         18,
			"amount":           money.NewBigIntFromInt64(1000000000000000000).String(),
			"usd_rate":         money.NewBigIntFromInt64(200000000000).String(),
			"chain_id":         int64(1),
			"tx_hash":          "0xidempotent",
			"block_number":     int64(12345678),
			"from_address":     "0xsender",
			"contract_address": "",
			"occurred_at":      time.Now().Add(-time.Hour).Format(time.RFC3339),
			"unique_id":        externalID,
		},
	)

	// Either it returns an error or returns the same transaction (depends on implementation)
	if err != nil {
		// Expected: duplicate error
		assert.Contains(t, err.Error(), "duplicate", "Should reject duplicate external_id")
	} else {
		// Or it returns the existing transaction
		assert.Equal(t, tx1.ID, tx2.ID, "Should return the same transaction for duplicate")
	}

	// Verify balance is only 1 ETH (not doubled)
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)
	expectedBalance := big.NewInt(1000000000000000000) // 1 ETH
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance), "Balance should be 1 ETH (not duplicated)")
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
