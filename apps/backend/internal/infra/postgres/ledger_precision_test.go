//go:build integration

package postgres

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Precision Tests for Ledger Repository
// These tests verify that NUMERIC(78,0) precision is maintained through the system
// Note: testDB is defined in ledger_repo_integration_test.go

// Helper to create a test user for precision tests
func createPrecisionTestUser(t *testing.T, ctx context.Context) uuid.UUID {
	userID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
	`, userID, "precision-"+userID.String()[:8]+"@example.com", "hash")
	require.NoError(t, err)
	return userID
}

// Helper to create a test wallet for precision tests
func createPrecisionTestWallet(t *testing.T, ctx context.Context, userID uuid.UUID) uuid.UUID {
	walletID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, chain_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, walletID, userID, "Precision Wallet "+walletID.String()[:8], "ethereum")
	require.NoError(t, err)
	return walletID
}

// TestLedgerRepository_Precision_MaxValue tests storing and loading 10^78 - 1
// This is the maximum value that can be stored in NUMERIC(78,0)
func TestLedgerRepository_Precision_MaxValue(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Create 10^78 - 1 (78 nines)
	maxValue := new(big.Int)
	maxValue.Exp(big.NewInt(10), big.NewInt(78), nil)
	maxValue.Sub(maxValue, big.NewInt(1))

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	// Create balance with max value
	balance := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "BTC",
		Balance:     new(big.Int).Set(maxValue),
		USDValue:    big.NewInt(0),
		LastUpdated: time.Now(),
	}
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance))

	// Retrieve and verify
	retrieved, err := repo.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	assert.Equal(t, 0, retrieved.Balance.Cmp(maxValue),
		"Max value not preserved: expected %s, got %s",
		maxValue.String(), retrieved.Balance.String())
}

// TestLedgerRepository_Precision_LargeValue tests a large but realistic value
// 10^36 is approximately 1 trillion trillion (10^24) units with 12 decimals
func TestLedgerRepository_Precision_LargeValue(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Create a large realistic value: 10^36
	largeValue := new(big.Int)
	largeValue.Exp(big.NewInt(10), big.NewInt(36), nil)

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".ETH",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "ETH",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	// Create balance
	balance := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "ETH",
		Balance:     new(big.Int).Set(largeValue),
		USDValue:    big.NewInt(0),
		LastUpdated: time.Now(),
	}
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance))

	// Retrieve and verify
	retrieved, err := repo.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)

	assert.Equal(t, 0, retrieved.Balance.Cmp(largeValue),
		"Large value not preserved: expected %s, got %s",
		largeValue.String(), retrieved.Balance.String())
}

// TestLedgerRepository_Precision_RoundTrip_256bit tests 2^256-1 precision
// This is important for Ethereum which uses 256-bit integers
func TestLedgerRepository_Precision_RoundTrip_256bit(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// 2^256 - 1 (max uint256 value, used in Ethereum)
	// This is approximately 1.157 * 10^77, which fits in NUMERIC(78,0)
	maxUint256 := new(big.Int)
	maxUint256.Exp(big.NewInt(2), big.NewInt(256), nil)
	maxUint256.Sub(maxUint256, big.NewInt(1))

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".WETH",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "WETH",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	// Create balance
	balance := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "WETH",
		Balance:     new(big.Int).Set(maxUint256),
		USDValue:    big.NewInt(0),
		LastUpdated: time.Now(),
	}
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance))

	// Retrieve and verify
	retrieved, err := repo.GetAccountBalance(ctx, account.ID, "WETH")
	require.NoError(t, err)

	assert.Equal(t, 0, retrieved.Balance.Cmp(maxUint256),
		"2^256-1 value not preserved: expected %s, got %s",
		maxUint256.String(), retrieved.Balance.String())
}

// TestLedgerRepository_Precision_EntryAmount tests entry amount precision
func TestLedgerRepository_Precision_EntryAmount(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Large amount in wei (18 decimals)
	// 1 trillion ETH in wei = 10^12 * 10^18 = 10^30
	largeAmount := new(big.Int)
	largeAmount.Exp(big.NewInt(10), big.NewInt(30), nil)

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".ETH",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "ETH",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.ETH",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "ETH",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))

	// Create transaction with large amounts
	txID := uuid.New()
	now := time.Now()

	tx := &ledger.Transaction{
		ID:         txID,
		Type:       ledger.TxTypeManualIncome,
		Source:     "test",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   map[string]interface{}{},
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: txID,
				AccountID:     account.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        new(big.Int).Set(largeAmount),
				AssetID:       "ETH",
				USDRate:       big.NewInt(200000000000), // $2000 * 10^8
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      map[string]interface{}{},
			},
			{
				ID:            uuid.New(),
				TransactionID: txID,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        new(big.Int).Set(largeAmount),
				AssetID:       "ETH",
				USDRate:       big.NewInt(200000000000),
				USDValue:      big.NewInt(0),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      map[string]interface{}{},
			},
		},
	}

	require.NoError(t, repo.CreateTransaction(ctx, tx))

	// Retrieve entries and verify
	entries, err := repo.GetEntriesByTransaction(ctx, txID)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	for _, entry := range entries {
		assert.Equal(t, 0, entry.Amount.Cmp(largeAmount),
			"Entry amount not preserved: expected %s, got %s",
			largeAmount.String(), entry.Amount.String())
	}
}

// TestLedgerRepository_USD_LargeRates tests very large USD rates
func TestLedgerRepository_USD_LargeRates(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Very large USD rate: $1 trillion per unit, scaled by 10^8
	// = 10^12 * 10^8 = 10^20
	largeRate := new(big.Int)
	largeRate.Exp(big.NewInt(10), big.NewInt(20), nil)

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".RARE",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "RARE",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.RARE",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "RARE",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))

	// Amount: 1 unit (no decimals for simplicity)
	amount := big.NewInt(1)

	// USD value = amount * rate / 10^8 = 10^12 (1 trillion dollars)
	usdValue := new(big.Int)
	usdValue.Mul(amount, largeRate)
	usdValue.Div(usdValue, big.NewInt(100000000)) // divide by 10^8

	txID := uuid.New()
	now := time.Now()

	tx := &ledger.Transaction{
		ID:         txID,
		Type:       ledger.TxTypeManualIncome,
		Source:     "test",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   map[string]interface{}{},
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: txID,
				AccountID:     account.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        amount,
				AssetID:       "RARE",
				USDRate:       new(big.Int).Set(largeRate),
				USDValue:      new(big.Int).Set(usdValue),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      map[string]interface{}{},
			},
			{
				ID:            uuid.New(),
				TransactionID: txID,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        amount,
				AssetID:       "RARE",
				USDRate:       new(big.Int).Set(largeRate),
				USDValue:      new(big.Int).Set(usdValue),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      map[string]interface{}{},
			},
		},
	}

	require.NoError(t, repo.CreateTransaction(ctx, tx))

	// Retrieve entries and verify
	entries, err := repo.GetEntriesByTransaction(ctx, txID)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	for _, entry := range entries {
		assert.Equal(t, 0, entry.USDRate.Cmp(largeRate),
			"USD rate not preserved: expected %s, got %s",
			largeRate.String(), entry.USDRate.String())
		assert.Equal(t, 0, entry.USDValue.Cmp(usdValue),
			"USD value not preserved: expected %s, got %s",
			usdValue.String(), entry.USDValue.String())
	}
}

// TestLedgerRepository_USD_SmallAmounts tests small amounts with large rates
func TestLedgerRepository_USD_SmallAmounts(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Small amount in wei: 1 wei (smallest unit of ETH)
	smallAmount := big.NewInt(1)

	// Large rate: $2000 per ETH scaled by 10^8 = 200000000000
	rate := big.NewInt(200000000000)

	// USD value for 1 wei: amount * rate / 10^8 / 10^18 (ETH decimals)
	// = 1 * 200000000000 / 10^8 / 10^18 = 2 * 10^-15 ≈ 0
	// For practical purposes, this rounds to 0
	usdValue := big.NewInt(0)

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".ETH",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "ETH",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.ETH",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "ETH",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))

	txID := uuid.New()
	now := time.Now()

	tx := &ledger.Transaction{
		ID:         txID,
		Type:       ledger.TxTypeManualIncome,
		Source:     "test",
		Status:     ledger.TransactionStatusCompleted,
		Version:    1,
		OccurredAt: now.Add(-time.Hour),
		RecordedAt: now,
		RawData:    map[string]interface{}{},
		Metadata:   map[string]interface{}{},
		Entries: []*ledger.Entry{
			{
				ID:            uuid.New(),
				TransactionID: txID,
				AccountID:     account.ID,
				DebitCredit:   ledger.Debit,
				EntryType:     ledger.EntryTypeAssetIncrease,
				Amount:        new(big.Int).Set(smallAmount),
				AssetID:       "ETH",
				USDRate:       new(big.Int).Set(rate),
				USDValue:      new(big.Int).Set(usdValue),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      map[string]interface{}{},
			},
			{
				ID:            uuid.New(),
				TransactionID: txID,
				AccountID:     incomeAccount.ID,
				DebitCredit:   ledger.Credit,
				EntryType:     ledger.EntryTypeIncome,
				Amount:        new(big.Int).Set(smallAmount),
				AssetID:       "ETH",
				USDRate:       new(big.Int).Set(rate),
				USDValue:      new(big.Int).Set(usdValue),
				OccurredAt:    now.Add(-time.Hour),
				CreatedAt:     now,
				Metadata:      map[string]interface{}{},
			},
		},
	}

	require.NoError(t, repo.CreateTransaction(ctx, tx))

	// Retrieve entries and verify
	entries, err := repo.GetEntriesByTransaction(ctx, txID)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	for _, entry := range entries {
		assert.Equal(t, 0, entry.Amount.Cmp(smallAmount),
			"Small amount not preserved: expected %s, got %s",
			smallAmount.String(), entry.Amount.String())
	}
}

// TestLedgerRepository_Precision_CalculateBalanceFromEntries tests that
// balance calculation from entries maintains precision
func TestLedgerRepository_Precision_CalculateBalanceFromEntries(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Use a large amount
	amount := new(big.Int)
	amount.Exp(big.NewInt(10), big.NewInt(25), nil) // 10^25

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	incomeAccount := &ledger.Account{
		ID:        uuid.New(),
		Code:      "income.BTC",
		Type:      ledger.AccountTypeIncome,
		AssetID:   "BTC",
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, incomeAccount))

	// Create multiple transactions
	numTransactions := 5
	expectedTotal := new(big.Int)

	for i := 0; i < numTransactions; i++ {
		txID := uuid.New()
		now := time.Now()

		tx := &ledger.Transaction{
			ID:         txID,
			Type:       ledger.TxTypeManualIncome,
			Source:     "test",
			Status:     ledger.TransactionStatusCompleted,
			Version:    1,
			OccurredAt: now.Add(-time.Duration(numTransactions-i) * time.Hour),
			RecordedAt: now,
			RawData:    map[string]interface{}{},
			Metadata:   map[string]interface{}{},
			Entries: []*ledger.Entry{
				{
					ID:            uuid.New(),
					TransactionID: txID,
					AccountID:     account.ID,
					DebitCredit:   ledger.Debit,
					EntryType:     ledger.EntryTypeAssetIncrease,
					Amount:        new(big.Int).Set(amount),
					AssetID:       "BTC",
					USDRate:       big.NewInt(5000000000000),
					USDValue:      big.NewInt(0),
					OccurredAt:    now.Add(-time.Duration(numTransactions-i) * time.Hour),
					CreatedAt:     now,
					Metadata:      map[string]interface{}{},
				},
				{
					ID:            uuid.New(),
					TransactionID: txID,
					AccountID:     incomeAccount.ID,
					DebitCredit:   ledger.Credit,
					EntryType:     ledger.EntryTypeIncome,
					Amount:        new(big.Int).Set(amount),
					AssetID:       "BTC",
					USDRate:       big.NewInt(5000000000000),
					USDValue:      big.NewInt(0),
					OccurredAt:    now.Add(-time.Duration(numTransactions-i) * time.Hour),
					CreatedAt:     now,
					Metadata:      map[string]interface{}{},
				},
			},
		}

		require.NoError(t, repo.CreateTransaction(ctx, tx))
		expectedTotal.Add(expectedTotal, amount)
	}

	// Calculate balance from entries
	calculated, err := repo.CalculateBalanceFromEntries(ctx, account.ID, "BTC")
	require.NoError(t, err)

	assert.Equal(t, 0, calculated.Cmp(expectedTotal),
		"Calculated balance doesn't match: expected %s, got %s",
		expectedTotal.String(), calculated.String())
}

// TestLedgerRepository_Precision_AdditionOverflow tests that adding large values
// doesn't cause overflow
func TestLedgerRepository_Precision_AdditionOverflow(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Use half of max value to test addition
	halfMax := new(big.Int)
	halfMax.Exp(big.NewInt(10), big.NewInt(38), nil) // 10^38

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	// Create initial balance
	balance := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "BTC",
		Balance:     new(big.Int).Set(halfMax),
		USDValue:    big.NewInt(0),
		LastUpdated: time.Now(),
	}
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance))

	// Add another half
	newBalance := new(big.Int).Add(halfMax, halfMax)
	balance.Balance = newBalance
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance))

	// Retrieve and verify
	retrieved, err := repo.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	expected := new(big.Int).Mul(halfMax, big.NewInt(2))
	assert.Equal(t, 0, retrieved.Balance.Cmp(expected),
		"Balance after addition doesn't match: expected %s, got %s",
		expected.String(), retrieved.Balance.String())
}

// TestLedgerRepository_Precision_SpecificBitcoinAmount tests realistic Bitcoin amounts
func TestLedgerRepository_Precision_SpecificBitcoinAmount(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Bitcoin: 21 million max supply * 10^8 satoshi
	// = 21,000,000 * 100,000,000 = 2,100,000,000,000,000 (2.1 quadrillion satoshi)
	maxBitcoinInSatoshi := new(big.Int)
	maxBitcoinInSatoshi.SetString("2100000000000000", 10)

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".BTC",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "BTC",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	// Create balance with max Bitcoin supply
	balance := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "BTC",
		Balance:     new(big.Int).Set(maxBitcoinInSatoshi),
		USDValue:    big.NewInt(0),
		LastUpdated: time.Now(),
	}
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance))

	// Retrieve and verify
	retrieved, err := repo.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	assert.Equal(t, 0, retrieved.Balance.Cmp(maxBitcoinInSatoshi),
		"Bitcoin amount not preserved: expected %s, got %s",
		maxBitcoinInSatoshi.String(), retrieved.Balance.String())
}

// TestLedgerRepository_Precision_SpecificEthereumAmount tests realistic Ethereum amounts
func TestLedgerRepository_Precision_SpecificEthereumAmount(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := NewLedgerRepository(testDB.Pool)
	userID := createPrecisionTestUser(t, ctx)
	walletID := createPrecisionTestWallet(t, ctx, userID)

	// Ethereum: ~120 million total supply * 10^18 wei
	// = 120,000,000 * 10^18 = 1.2 * 10^26 wei
	maxEthInWei := new(big.Int)
	maxEthInWei.SetString("120000000000000000000000000", 10) // 120 million ETH in wei

	// Create account
	account := &ledger.Account{
		ID:        uuid.New(),
		Code:      "wallet." + walletID.String() + ".ETH",
		Type:      ledger.AccountTypeCryptoWallet,
		AssetID:   "ETH",
		WalletID:  &walletID,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{},
	}
	require.NoError(t, repo.CreateAccount(ctx, account))

	// Create balance with large ETH amount
	balance := &ledger.AccountBalance{
		AccountID:   account.ID,
		AssetID:     "ETH",
		Balance:     new(big.Int).Set(maxEthInWei),
		USDValue:    big.NewInt(0),
		LastUpdated: time.Now(),
	}
	require.NoError(t, repo.UpsertAccountBalance(ctx, balance))

	// Retrieve and verify
	retrieved, err := repo.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)

	assert.Equal(t, 0, retrieved.Balance.Cmp(maxEthInWei),
		"Ethereum amount not preserved: expected %s, got %s",
		maxEthInWei.String(), retrieved.Balance.String())
}
