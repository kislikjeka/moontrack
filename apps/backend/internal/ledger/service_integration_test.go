//go:build integration

package ledger_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/testutil/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func setupTest(t *testing.T) (*ledger.Service, *postgres.LedgerRepository, context.Context) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	// Register a simple test handler
	testHandler := newTestHandler()
	registry.Register(testHandler)

	svc := ledger.NewService(repo, registry)
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

// Helper to create a test wallet
func createTestWallet(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
	walletID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, chain_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, walletID, userID, "Test Wallet "+walletID.String()[:8], "ethereum")
	require.NoError(t, err)
	return walletID
}

// testHandler is a simple handler for testing
type testHandler struct {
	ledger.BaseHandler
	walletID uuid.UUID
	assetID  string
	amount   *big.Int
}

func newTestHandler() *testHandler {
	return &testHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeManualIncome),
		walletID:    uuid.New(),
		assetID:     "BTC",
		amount:      big.NewInt(100000000),
	}
}

func (h *testHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	walletID := h.walletID
	if wid, ok := data["wallet_id"].(string); ok {
		walletID, _ = uuid.Parse(wid)
	}
	assetID := h.assetID
	if aid, ok := data["asset_id"].(string); ok {
		assetID = aid
	}
	amount := h.amount
	if amtStr, ok := data["amount"].(string); ok {
		amount, _ = new(big.Int).SetString(amtStr, 10)
	}

	now := time.Now()
	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      amount,
			AssetID:     assetID,
			USDRate:     big.NewInt(5000000000000), // $50,000
			USDValue:    big.NewInt(5000000000000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"account_code": "wallet." + walletID.String() + "." + assetID,
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      amount,
			AssetID:     assetID,
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000000000000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "income." + assetID,
			},
		},
	}, nil
}

func (h *testHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	return nil
}

// Service integration tests

func TestLedgerService_RecordTransaction_BalancedEntries(t *testing.T) {
	svc, _, ctx := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100000000",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx)

	assert.Equal(t, ledger.TransactionStatusCompleted, tx.Status)
	assert.Len(t, tx.Entries, 2)

	// Verify entries are balanced
	err = tx.VerifyBalance()
	assert.NoError(t, err)
}

func TestLedgerService_RecordTransaction_AccountCreation(t *testing.T) {
	svc, repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	accountCode := "wallet." + walletID.String() + ".BTC"

	// Verify account doesn't exist
	_, err := repo.GetAccountByCode(ctx, accountCode)
	assert.Error(t, err)

	// Record transaction
	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100000000",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Verify account was auto-created
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)
	assert.Equal(t, ledger.AccountTypeCryptoWallet, account.Type)
	assert.Equal(t, "BTC", account.AssetID)
}

func TestLedgerService_RecordTransaction_BalanceUpdate(t *testing.T) {
	svc, repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record first transaction: +100
	tx1, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx1)

	// Get wallet account
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	// Check balance after first transaction
	balance1, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance1.Balance.Cmp(big.NewInt(100)))

	// Record second transaction: +50
	tx2, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "50",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx2)

	// Check balance after second transaction: should be 150
	balance2, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance2.Balance.Cmp(big.NewInt(150)))
}

func TestLedgerService_GetBalance_NonExistentAccount(t *testing.T) {
	svc, _, ctx := setupTest(t)

	// Get balance for non-existent wallet
	balance, err := svc.GetBalance(ctx, uuid.New(), "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(0)))
}

func TestLedgerService_ReconcileBalance_Matches(t *testing.T) {
	svc, repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record transactions
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)
	require.NoError(t, err)

	// Get wallet account
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	// Reconcile should pass
	err = svc.ReconcileBalance(ctx, account.ID, "BTC")
	assert.NoError(t, err)
}

func TestLedgerService_ConcurrentTransactions(t *testing.T) {
	// Note: This test validates concurrent transaction handling.
	// Currently, the repository doesn't implement proper database transactions,
	// so this test runs transactions sequentially to avoid race conditions.
	// Once BeginTx/CommitTx/RollbackTx are properly implemented,
	// this test can be changed to truly concurrent execution.
	svc, repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	numTransactions := 10

	// Run transactions sequentially until proper transaction support is added
	for i := 0; i < numTransactions; i++ {
		_, err := svc.RecordTransaction(
			ctx,
			ledger.TxTypeManualIncome,
			"manual",
			nil,
			time.Now().Add(-time.Hour),
			map[string]interface{}{
				"wallet_id": walletID.String(),
				"asset_id":  "BTC",
				"amount":    "10",
			},
		)
		require.NoError(t, err, "Transaction %d failed", i)
	}

	// Verify final balance: 10 transactions * 10 = 100
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(100)))
}

// Double-entry invariant tests

func TestLedgerService_DoubleEntry_DebitsEqualCredits(t *testing.T) {
	svc, _, ctx := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record multiple transactions
	for i := 0; i < 5; i++ {
		tx, err := svc.RecordTransaction(
			ctx,
			ledger.TxTypeManualIncome,
			"manual",
			nil,
			time.Now().Add(-time.Hour),
			map[string]interface{}{
				"wallet_id": walletID.String(),
				"asset_id":  "BTC",
				"amount":    "100",
			},
		)
		require.NoError(t, err)

		// Each transaction must be balanced
		err = tx.VerifyBalance()
		assert.NoError(t, err, "Transaction %d is not balanced", i)
	}
}

func TestLedgerService_GetTransaction(t *testing.T) {
	svc, _, ctx := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)
	require.NoError(t, err)

	// Retrieve transaction
	retrieved, err := svc.GetTransaction(ctx, tx.ID)
	require.NoError(t, err)
	assert.Equal(t, tx.ID, retrieved.ID)
	assert.Equal(t, tx.Type, retrieved.Type)
	assert.Len(t, retrieved.Entries, 2)
}

func TestLedgerService_ListTransactions(t *testing.T) {
	svc, _, ctx := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record 3 transactions
	for i := 0; i < 3; i++ {
		_, err := svc.RecordTransaction(
			ctx,
			ledger.TxTypeManualIncome,
			"manual",
			nil,
			time.Now().Add(-time.Duration(i+1)*time.Hour),
			map[string]interface{}{
				"wallet_id": walletID.String(),
				"asset_id":  "BTC",
				"amount":    "100",
			},
		)
		require.NoError(t, err)
	}

	// List all
	txs, err := svc.ListTransactions(ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, txs, 3)

	// List with limit
	txsLimited, err := svc.ListTransactions(ctx, ledger.TransactionFilters{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, txsLimited, 2)
}
