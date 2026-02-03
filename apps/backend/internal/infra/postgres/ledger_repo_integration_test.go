//go:build integration

package postgres

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
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

func setupTest(t *testing.T) (*LedgerRepository, context.Context) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	return repo, ctx
}

// Helper to create a test user
func createTestUser(t *testing.T, ctx context.Context) uuid.UUID {
	userID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
	`, userID, "test-"+userID.String()[:8]+"@example.com", "hash")
	require.NoError(t, err)
	return userID
}

// Helper to create a test wallet
func createTestWallet(t *testing.T, ctx context.Context, userID uuid.UUID) uuid.UUID {
	walletID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, chain_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, walletID, userID, "Test Wallet "+walletID.String()[:8], "ethereum")
	require.NoError(t, err)
	return walletID
}

// Account tests

func TestLedgerRepository_CreateAccount_Success(t *testing.T) {
	repo, ctx := setupTest(t)

	// Create user and wallet first
	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{"test": true},
	}

	err := repo.CreateAccount(ctx, account)
	require.NoError(t, err)

	// Verify it was created
	retrieved, err := repo.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	assert.Equal(t, account.Code, retrieved.Code)
	assert.Equal(t, account.Type, retrieved.Type)
	assert.Equal(t, account.AssetID, retrieved.AssetID)
	assert.Equal(t, *account.WalletID, *retrieved.WalletID)
}

func TestLedgerRepository_CreateAccount_DuplicateCode(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	err := repo.CreateAccount(ctx, account)
	require.NoError(t, err)

	// Try to create with same code
	account2 := &ledger.Account{
		ID:        uuid.New(),
		Code:      account.Code, // Same code
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	err = repo.CreateAccount(ctx, account2)
	assert.Error(t, err)
}

func TestLedgerRepository_GetAccountByCode_Exists(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".ETH",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "ETH",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	err := repo.CreateAccount(ctx, account)
	require.NoError(t, err)

	retrieved, err := repo.GetAccountByCode(ctx, account.Code)
	require.NoError(t, err)
	assert.Equal(t, account.ID, retrieved.ID)
}

func TestLedgerRepository_GetAccountByCode_NotExists(t *testing.T) {
	repo, ctx := setupTest(t)

	_, err := repo.GetAccountByCode(ctx, "non-existent-code")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLedgerRepository_CreateAccount_IncomeAccount(t *testing.T) {
	repo, ctx := setupTest(t)

	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.BTC",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "BTC",
		WalletID:  nil, // Income accounts don't have wallet
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	err := repo.CreateAccount(ctx, account)
	require.NoError(t, err)

	retrieved, err := repo.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	assert.Equal(t, ledger.AccountTypeIncome, retrieved.Type)
	assert.Nil(t, retrieved.WalletID)
}

// Transaction tests

func TestLedgerRepository_CreateTransaction_WithEntries(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// Create accounts
	walletAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.BTC",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "BTC",
		WalletID:  nil,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, walletAccount))
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))

	now := time.Now()
	amount := big.NewInt(100000000) // 1 BTC in satoshi
	usdRate := big.NewInt(5000000000000) // $50,000 scaled by 10^8
	usdValue := big.NewInt(5000000000000)

	tx := &ledger.Transaction{
		ID:         uuid.New(),
		Type:       ledger.TxTypeManualIncome,
		Source:     "manual",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{"test": true},
		Metadata:   make(map[string]interface{}),
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil, // Will be set
				AccountID:     walletAccount.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        amount,
				AssetID:       "BTC",
				USDRate:       usdRate,
				USDValue:      usdValue,
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        amount,
				AssetID:       "BTC",
				USDRate:       usdRate,
				USDValue:      usdValue,
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
		},
	}

	// Set transaction ID on entries
	for _, entry := range tx.Entries {
		entry.TransactionID = tx.ID
	}

	err := repo.CreateTransaction(ctx, tx)
	require.NoError(t, err)

	// Verify transaction
	retrieved, err := repo.GetTransaction(ctx, tx.ID)
	require.NoError(t, err)
	assert.Equal(t, tx.Type, retrieved.Type)
	assert.Equal(t, tx.Source, retrieved.Source)
	assert.Equal(t, tx.Status, retrieved.Status)
	assert.Len(t, retrieved.Entries, 2)
}

func TestLedgerRepository_GetTransaction_WithEntries(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// Create accounts
	walletAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".ETH",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "ETH",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.ETH",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "ETH",
		WalletID:  nil,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, walletAccount))
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))

	now := time.Now()
	amount := big.NewInt(1000000000000000000) // 1 ETH in wei

	tx := &ledger.Transaction{
		ID:         uuid.New(),
		Type:       ledger.TxTypeManualIncome,
		Source:     "manual",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{"asset": "ETH"},
		Metadata:   make(map[string]interface{}),
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     walletAccount.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        amount,
				AssetID:       "ETH",
				USDRate:       big.NewInt(300000000000), // $3000
				USDValue:      big.NewInt(300000000000),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        amount,
				AssetID:       "ETH",
				USDRate:       big.NewInt(300000000000),
				USDValue:      big.NewInt(300000000000),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
		},
	}

	for _, entry := range tx.Entries {
		entry.TransactionID = tx.ID
	}

	require.NoError(t, repo.CreateTransaction(ctx, tx))

	// Retrieve and verify
	retrieved, err := repo.GetTransaction(ctx, tx.ID)
	require.NoError(t, err)
	require.Len(t, retrieved.Entries, 2)

	// Check entries have correct amounts
	for _, entry := range retrieved.Entries {
		assert.Equal(t, 0, entry.Amount.Cmp(amount))
	}
}

func TestLedgerRepository_ListTransactions_Filters(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// Create accounts
	walletAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.BTC",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "BTC",
		WalletID:  nil,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	expenseAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "expense.BTC",
		Type:      ledger.AccountTypeExpense,
		AssetID:   "BTC",
		WalletID:  nil,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, walletAccount))
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))
	require.NoError(t, repo.CreateAccount(ctx, expenseAccount))

	now := time.Now()
	amount := big.NewInt(100000000)

	// Create income transaction
	tx1 := &ledger.Transaction{
		ID:         uuid.New(),
		Type:       ledger.TxTypeManualIncome,
		Source:     "manual",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-2 * time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   make(map[string]interface{}),
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     walletAccount.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        amount,
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-2 * time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        amount,
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-2 * time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
		},
	}
	for _, e := range tx1.Entries {
		e.TransactionID = tx1.ID
	}

	// Create outcome transaction
	tx2 := &ledger.Transaction{
		ID:         uuid.New(),
		Type:       ledger.TxTypeManualOutcome,
		Source:     "manual",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-1 * time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   make(map[string]interface{}),
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     walletAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeAssetDecrease,
				Amount:        big.NewInt(50000000),
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-1 * time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     expenseAccount.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeExpense,
				Amount:        big.NewInt(50000000),
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-1 * time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
		},
	}
	for _, e := range tx2.Entries {
		e.TransactionID = tx2.ID
	}

	require.NoError(t, repo.CreateTransaction(ctx, tx1))
	require.NoError(t, repo.CreateTransaction(ctx, tx2))

	// List all
	all, err := repo.ListTransactions(ctx, ledger.TransactionFilters{})
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// Filter by type
	txType := string(ledger.TxTypeManualIncome)
	incomes, err := repo.ListTransactions(ctx, ledger.TransactionFilters{Type: &txType})
	require.NoError(t, err)
	assert.Len(t, incomes, 1)
	assert.Equal(t, ledger.TxTypeManualIncome, incomes[0].Type)

	// Filter by limit
	limited, err := repo.ListTransactions(ctx, ledger.TransactionFilters{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, limited, 1)
}

// Balance tests

func TestLedgerRepository_GetAccountBalance_ZeroInitial(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	// Get balance for account with no entries
	balance, err := repo.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(0)))
}

func TestLedgerRepository_UpsertAccountBalance_Insert(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	balance := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "BTC",
		Balance:     big.NewInt(100000000),
		USDValue:    big.NewInt(5000000000000),
		LastUpdated: time.Now(),
	}

	err := repo.UpsertAccountBalance(ctx, balance)
	require.NoError(t, err)

	// Verify
	retrieved, err := repo.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, retrieved.Balance.Cmp(big.NewInt(100000000)))
}

func TestLedgerRepository_UpsertAccountBalance_Update(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	// Insert initial balance
	balance1 := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "BTC",
		Balance:     big.NewInt(100000000),
		USDValue:    big.NewInt(5000000000000),
		LastUpdated: time.Now(),
	}
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance1))

	// Update balance
	balance2 := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "BTC",
		Balance:     big.NewInt(200000000),
		USDValue:    big.NewInt(10000000000000),
		LastUpdated: time.Now(),
	}
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance2))

	// Verify update
	retrieved, err := repo.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, retrieved.Balance.Cmp(big.NewInt(200000000)))
}

func TestLedgerRepository_CalculateBalanceFromEntries(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// Create accounts
	walletAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.BTC",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "BTC",
		WalletID:  nil,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	expenseAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "expense.BTC",
		Type:      ledger.AccountTypeExpense,
		AssetID:   "BTC",
		WalletID:  nil,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, walletAccount))
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))
	require.NoError(t, repo.CreateAccount(ctx, expenseAccount))

	now := time.Now()

	// Income: +100 BTC
	tx1 := &ledger.Transaction{
		ID:         uuid.New(),
		Type:       ledger.TxTypeManualIncome,
		Source:     "manual",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-2 * time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   make(map[string]interface{}),
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     walletAccount.ID,
				DebitCredit:   ledger.Debit, // +100
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        big.NewInt(100),
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-2 * time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        big.NewInt(100),
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-2 * time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
		},
	}
	for _, e := range tx1.Entries {
		e.TransactionID = tx1.ID
	}

	// Outcome: -30 BTC
	tx2 := &ledger.Transaction{
		ID:         uuid.New(),
		Type:       ledger.TxTypeManualOutcome,
		Source:     "manual",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-1 * time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   make(map[string]interface{}),
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     walletAccount.ID,
				DebitCredit:   ledger.Credit, // -30
				EntryType:     ledger.EntryTypeAssetDecrease,
				Amount:        big.NewInt(30),
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-1 * time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     expenseAccount.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeExpense,
				Amount:        big.NewInt(30),
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-1 * time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
		},
	}
	for _, e := range tx2.Entries {
		e.TransactionID = tx2.ID
	}

	require.NoError(t, repo.CreateTransaction(ctx, tx1))
	require.NoError(t, repo.CreateTransaction(ctx, tx2))

	// Calculate balance from entries: should be 100 - 30 = 70
	balance, err := repo.CalculateBalanceFromEntries(ctx, walletAccount.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(70)))
}

func TestLedgerRepository_NUMERIC78_Precision(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// Create accounts
	walletAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".ETH",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "ETH",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.ETH",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "ETH",
		WalletID:  nil,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, walletAccount))
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))

	// Very large number: 50-digit number to test precision
	largeAmount := new(big.Int)
	largeAmount.SetString("12345678901234567890123456789012345678901234567890", 10)

	now := time.Now()

	tx := &ledger.Transaction{
		ID:         uuid.New(),
		Type:       ledger.TxTypeManualIncome,
		Source:     "manual",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   make(map[string]interface{}),
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     walletAccount.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        largeAmount,
				AssetID:       "ETH",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        largeAmount,
				AssetID:       "ETH",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
		},
	}
	for _, e := range tx.Entries {
		e.TransactionID = tx.ID
	}

	require.NoError(t, repo.CreateTransaction(ctx, tx))

	// Retrieve and verify precision preserved
	retrieved, err := repo.GetTransaction(ctx, tx.ID)
	require.NoError(t, err)
	require.Len(t, retrieved.Entries, 2)

	for _, entry := range retrieved.Entries {
		assert.Equal(t, 0, entry.Amount.Cmp(largeAmount), "Large number precision should be preserved")
	}
}

func TestLedgerRepository_FindTransactionsBySource(t *testing.T) {
	repo, ctx := setupTest(t)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	walletAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.BTC",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "BTC",
		WalletID:  nil,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
	require.NoError(t, repo.CreateAccount(ctx, walletAccount))
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))

	now := time.Now()
	externalID := "tx-12345"

	tx := &ledger.Transaction{
		ID:         uuid.New(),
		Type:       ledger.TxTypeManualIncome,
		Source:     "etherscan",
		ExternalID: &externalID,
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   make(map[string]interface{}),
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     walletAccount.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        big.NewInt(100),
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
			{
				ID:            uuid.New(),
				TransactionID: uuid.Nil,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        big.NewInt(100),
				AssetID:       "BTC",
				USDRate:       big.NewInt(0),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      make(map[string]interface{}),
			},
		},
	}
	for _, e := range tx.Entries {
		e.TransactionID = tx.ID
	}

	require.NoError(t, repo.CreateTransaction(ctx, tx))

	// Find by source and external ID
	found, err := repo.FindTransactionsBySource(ctx, "etherscan", externalID)
	require.NoError(t, err)
	assert.Equal(t, tx.ID, found.ID)

	// Not found
	_, err = repo.FindTransactionsBySource(ctx, "etherscan", "non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
