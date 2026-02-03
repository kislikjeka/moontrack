//go:build integration

package manual_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/manual"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Entry Generation Validation Tests for Manual Handlers
// These tests verify proper entry generation including metadata, price sources, and precision
// Note: testDB and helpers are defined in handler_integration_test.go

// Helper to create a test user for validation tests
func createValidationTestUser(t *testing.T, ctx context.Context) uuid.UUID {
	userID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
	`, userID, "validation-"+userID.String()[:8]+"@example.com", "hash")
	require.NoError(t, err)
	return userID
}

// Helper to create a test wallet for validation tests
func createValidationTestWallet(t *testing.T, ctx context.Context, userID uuid.UUID) uuid.UUID {
	walletID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, chain_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
	`, walletID, userID, "Validation Wallet "+walletID.String()[:8], "ethereum")
	require.NoError(t, err)
	return walletID
}

// validationMockRegistryService is a mock for RegistryService (validation tests)
type validationMockRegistryService struct {
	currentPrice    *big.Int
	historicalPrice *big.Int
	err             error
}

func (m *validationMockRegistryService) GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.currentPrice, nil
}

func (m *validationMockRegistryService) GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.historicalPrice, nil
}

// validationMockWalletRepository is a mock for WalletRepository (validation tests)
type validationMockWalletRepository struct {
	wallet *wallet.Wallet
	err    error
}

func (m *validationMockWalletRepository) GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.wallet, nil
}

// validationMockBalanceGetter is a mock for BalanceGetter (validation tests)
type validationMockBalanceGetter struct {
	balance *big.Int
	err     error
}

func (m *validationMockBalanceGetter) GetBalance(ctx context.Context, walletID uuid.UUID, assetID string) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.balance, nil
}

// TestManualIncomeHandler_PriceSourceTracking_Manual tests that manual price source is correctly tracked
func TestManualIncomeHandler_PriceSourceTracking_Manual(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{
		currentPrice: big.NewInt(5000000000000), // $50,000 scaled by 10^8
	}

	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// Create transaction data with manual USD rate
	manualRate, _ := money.NewBigIntFromString("5500000000000") // $55,000 scaled by 10^8
	txn := &manual.ManualIncomeTransaction{
		WalletID:   walletID,
		AssetID:    "BTC",
		Decimals:   8,
		Amount:     money.NewBigIntFromInt64(100000000), // 1 BTC in satoshi
		USDRate:    manualRate,
		OccurredAt: time.Now().Add(-time.Hour),
		Notes:      "Test deposit",
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Verify price_source in metadata
	for _, entry := range entries {
		priceSource, ok := entry.Metadata["price_source"].(string)
		require.True(t, ok, "price_source should be present in metadata")
		assert.Equal(t, "manual", priceSource, "price_source should be 'manual' when USD rate is provided")
	}
}

// TestManualIncomeHandler_PriceSourceTracking_CoinGecko tests that coingecko price source is correctly tracked
func TestManualIncomeHandler_PriceSourceTracking_CoinGecko(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{
		currentPrice: big.NewInt(5000000000000), // $50,000 scaled by 10^8
	}

	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// Create transaction data WITHOUT manual USD rate
	txn := &manual.ManualIncomeTransaction{
		WalletID:     walletID,
		AssetID:      "BTC",
		PriceAssetID: "bitcoin",
		Decimals:     8,
		Amount:       money.NewBigIntFromInt64(100000000), // 1 BTC in satoshi
		USDRate:      nil,                                 // No manual rate - should fetch from CoinGecko
		OccurredAt:   time.Now(),                          // Today - should use current price
		Notes:        "Test deposit",
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Verify price_source in metadata
	for _, entry := range entries {
		priceSource, ok := entry.Metadata["price_source"].(string)
		require.True(t, ok, "price_source should be present in metadata")
		assert.Equal(t, "coingecko", priceSource, "price_source should be 'coingecko' when fetched from API")
	}
}

// TestManualIncomeHandler_MetadataFidelity tests that all metadata is preserved
func TestManualIncomeHandler_MetadataFidelity(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{
		currentPrice: big.NewInt(5000000000000),
	}

	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	testNotes := "Important deposit with specific notes"
	manualRate, _ := money.NewBigIntFromString("5000000000000")

	txn := &manual.ManualIncomeTransaction{
		WalletID:   walletID,
		AssetID:    "ETH",
		Decimals:   18,
		Amount:     money.NewBigIntFromInt64(1000000000000000000), // 1 ETH in wei
		USDRate:    manualRate,
		OccurredAt: time.Now().Add(-time.Hour),
		Notes:      testNotes,
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Find the wallet entry (debit entry with wallet_id)
	var walletEntry *ledger.Entry
	for _, entry := range entries {
		if entry.DebitCredit == ledger.Debit {
			walletEntry = entry
			break
		}
	}
	require.NotNil(t, walletEntry)

	// Verify wallet_id is preserved
	storedWalletID, ok := walletEntry.Metadata["wallet_id"].(string)
	require.True(t, ok, "wallet_id should be present in metadata")
	assert.Equal(t, walletID.String(), storedWalletID)

	// Verify notes are preserved
	storedNotes, ok := walletEntry.Metadata["notes"].(string)
	require.True(t, ok, "notes should be present in metadata")
	assert.Equal(t, testNotes, storedNotes)

	// Verify account_code is correct
	accountCode, ok := walletEntry.Metadata["account_code"].(string)
	require.True(t, ok, "account_code should be present in metadata")
	assert.Equal(t, "wallet."+walletID.String()+".ETH", accountCode)
}

// TestManualIncomeHandler_USDValuePrecision_6Decimals tests USD value calculation with 6 decimals (USDC)
func TestManualIncomeHandler_USDValuePrecision_6Decimals(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{}
	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// USDC: 6 decimals
	// Amount: 1000 USDC = 1000 * 10^6 = 1,000,000,000 base units
	amount, _ := money.NewBigIntFromString("1000000000")

	// USD rate for stablecoin: $1.00 = 100000000 (scaled by 10^8)
	rate, _ := money.NewBigIntFromString("100000000")

	txn := &manual.ManualIncomeTransaction{
		WalletID:   walletID,
		AssetID:    "USDC",
		Decimals:   6,
		Amount:     amount,
		USDRate:    rate,
		OccurredAt: time.Now().Add(-time.Hour),
		Notes:      "USDC deposit",
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Expected USD value: (amount * rate) / 10^(decimals + 8)
	// = (1000000000 * 100000000) / 10^14
	// = 100000000000000000 / 100000000000000
	// = 1000
	expectedUSDValue := big.NewInt(1000)

	assert.Equal(t, 0, entries[0].USDValue.Cmp(expectedUSDValue),
		"USD value incorrect for 6 decimals: expected %s, got %s",
		expectedUSDValue.String(), entries[0].USDValue.String())
}

// TestManualIncomeHandler_USDValuePrecision_8Decimals tests USD value calculation with 8 decimals (BTC)
func TestManualIncomeHandler_USDValuePrecision_8Decimals(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{}
	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// BTC: 8 decimals
	// Amount: 1 BTC = 100,000,000 satoshi
	amount := money.NewBigIntFromInt64(100000000)

	// USD rate: $50,000 = 5,000,000,000,000 (scaled by 10^8)
	rate, _ := money.NewBigIntFromString("5000000000000")

	txn := &manual.ManualIncomeTransaction{
		WalletID:   walletID,
		AssetID:    "BTC",
		Decimals:   8,
		Amount:     amount,
		USDRate:    rate,
		OccurredAt: time.Now().Add(-time.Hour),
		Notes:      "BTC deposit",
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Expected USD value: (amount * rate) / 10^(decimals + 8)
	// = (100000000 * 5000000000000) / 10^16
	// = 500000000000000000000 / 10000000000000000
	// = 50000
	expectedUSDValue := big.NewInt(50000)

	assert.Equal(t, 0, entries[0].USDValue.Cmp(expectedUSDValue),
		"USD value incorrect for 8 decimals: expected %s, got %s",
		expectedUSDValue.String(), entries[0].USDValue.String())
}

// TestManualIncomeHandler_USDValuePrecision_18Decimals tests USD value calculation with 18 decimals (ETH)
func TestManualIncomeHandler_USDValuePrecision_18Decimals(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{}
	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// ETH: 18 decimals
	// Amount: 10 ETH = 10 * 10^18 wei
	amount, _ := money.NewBigIntFromString("10000000000000000000")

	// USD rate: $2,000 = 200,000,000,000 (scaled by 10^8)
	rate, _ := money.NewBigIntFromString("200000000000")

	txn := &manual.ManualIncomeTransaction{
		WalletID:   walletID,
		AssetID:    "ETH",
		Decimals:   18,
		Amount:     amount,
		USDRate:    rate,
		OccurredAt: time.Now().Add(-time.Hour),
		Notes:      "ETH deposit",
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Expected USD value: (amount * rate) / 10^(decimals + 8)
	// = (10000000000000000000 * 200000000000) / 10^26
	// = 2000000000000000000000000000000 / 100000000000000000000000000
	// = 20000
	expectedUSDValue := big.NewInt(20000)

	assert.Equal(t, 0, entries[0].USDValue.Cmp(expectedUSDValue),
		"USD value incorrect for 18 decimals: expected %s, got %s",
		expectedUSDValue.String(), entries[0].USDValue.String())
}

// TestManualOutcomeHandler_EntryTypeConsistency tests that entry types match debit/credit
func TestManualOutcomeHandler_EntryTypeConsistency(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{}
	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	// Balance of 1000 units
	mockBalanceGetter := &validationMockBalanceGetter{
		balance: big.NewInt(1000),
	}

	handler := manual.NewManualOutcomeHandler(mockRegistry, mockWalletRepo, mockBalanceGetter)

	rate, _ := money.NewBigIntFromString("5000000000000")

	txn := &manual.ManualOutcomeTransaction{
		WalletID:   walletID,
		AssetID:    "BTC",
		Decimals:   8,
		Amount:     money.NewBigIntFromInt64(500), // Withdraw 500
		USDRate:    rate,
		OccurredAt: time.Now().Add(-time.Hour),
		Notes:      "Test withdrawal",
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Find wallet entry (CREDIT for outcome) and expense entry (DEBIT)
	var walletEntry, expenseEntry *ledger.Entry
	for _, entry := range entries {
		if entry.DebitCredit == ledger.Credit {
			walletEntry = entry
		} else {
			expenseEntry = entry
		}
	}

	require.NotNil(t, walletEntry, "Should have a credit entry for wallet")
	require.NotNil(t, expenseEntry, "Should have a debit entry for expense")

	// Verify entry types
	assert.Equal(t, ledger.EntryTypeAssetDecrease, walletEntry.EntryType,
		"Wallet entry should be asset_decrease for outcome")
	assert.Equal(t, ledger.EntryTypeExpense, expenseEntry.EntryType,
		"Expense entry should be expense type")

	// Verify amounts match
	assert.Equal(t, 0, walletEntry.Amount.Cmp(expenseEntry.Amount),
		"Amounts should match between entries")
}

// TestManualIncomeHandler_EntryTypeConsistency tests that entry types match debit/credit
func TestManualIncomeHandler_EntryTypeConsistency(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{}
	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	rate, _ := money.NewBigIntFromString("5000000000000")

	txn := &manual.ManualIncomeTransaction{
		WalletID:   walletID,
		AssetID:    "BTC",
		Decimals:   8,
		Amount:     money.NewBigIntFromInt64(500),
		USDRate:    rate,
		OccurredAt: time.Now().Add(-time.Hour),
		Notes:      "Test deposit",
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Find wallet entry (DEBIT for income) and income entry (CREDIT)
	var walletEntry, incomeEntry *ledger.Entry
	for _, entry := range entries {
		if entry.DebitCredit == ledger.Debit {
			walletEntry = entry
		} else {
			incomeEntry = entry
		}
	}

	require.NotNil(t, walletEntry, "Should have a debit entry for wallet")
	require.NotNil(t, incomeEntry, "Should have a credit entry for income")

	// Verify entry types
	assert.Equal(t, ledger.EntryTypeAssetIncrease, walletEntry.EntryType,
		"Wallet entry should be asset_increase for income")
	assert.Equal(t, ledger.EntryTypeIncome, incomeEntry.EntryType,
		"Income entry should be income type")

	// Verify amounts match
	assert.Equal(t, 0, walletEntry.Amount.Cmp(incomeEntry.Amount),
		"Amounts should match between entries")
}

// TestManualIncomeHandler_EntriesBalanced tests that generated entries always balance
func TestManualIncomeHandler_EntriesBalanced(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{}
	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	testCases := []struct {
		name     string
		amount   string
		decimals int
	}{
		{"small_amount", "1", 8},
		{"medium_amount", "100000000", 8},
		{"large_amount", "100000000000000000000", 18},
		{"odd_amount", "123456789012345678", 18},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			amount, ok := money.NewBigIntFromString(tc.amount)
			require.True(t, ok)

			rate, _ := money.NewBigIntFromString("5000000000000")

			txn := &manual.ManualIncomeTransaction{
				WalletID:   walletID,
				AssetID:    "TEST",
				Decimals:   tc.decimals,
				Amount:     amount,
				USDRate:    rate,
				OccurredAt: time.Now().Add(-time.Hour),
				Notes:      "Test",
			}

			entries, err := handler.GenerateEntries(ctx, txn)
			require.NoError(t, err)
			require.Len(t, entries, 2)

			// Sum debits and credits
			debitSum := big.NewInt(0)
			creditSum := big.NewInt(0)

			for _, entry := range entries {
				if entry.DebitCredit == ledger.Debit {
					debitSum.Add(debitSum, entry.Amount)
				} else {
					creditSum.Add(creditSum, entry.Amount)
				}
			}

			assert.Equal(t, 0, debitSum.Cmp(creditSum),
				"Entries should balance: debit=%s, credit=%s",
				debitSum.String(), creditSum.String())
		})
	}
}

// TestManualOutcomeHandler_BalanceValidation tests that outcome handler validates balance
func TestManualOutcomeHandler_BalanceValidation(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{}
	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	// Balance of only 100 units
	mockBalanceGetter := &validationMockBalanceGetter{
		balance: big.NewInt(100),
	}

	handler := manual.NewManualOutcomeHandler(mockRegistry, mockWalletRepo, mockBalanceGetter)

	rate, _ := money.NewBigIntFromString("5000000000000")

	// Try to withdraw more than available
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "500", // More than balance of 100
		"usd_rate":    rate.String(),
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"notes":       "Test withdrawal",
	}

	err := handler.ValidateData(ctx, data)
	assert.Error(t, err)
	assert.ErrorIs(t, err, manual.ErrInsufficientBalance)
}

// TestManualIncomeHandler_AccountCodeFormat tests that account codes are correctly formatted
func TestManualIncomeHandler_AccountCodeFormat(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	userID := createValidationTestUser(t, ctx)
	walletID := createValidationTestWallet(t, ctx, userID)

	mockRegistry := &validationMockRegistryService{}
	mockWalletRepo := &validationMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	rate, _ := money.NewBigIntFromString("5000000000000")

	txn := &manual.ManualIncomeTransaction{
		WalletID:   walletID,
		AssetID:    "BTC",
		Decimals:   8,
		Amount:     money.NewBigIntFromInt64(100),
		USDRate:    rate,
		OccurredAt: time.Now().Add(-time.Hour),
		Notes:      "Test",
	}

	entries, err := handler.GenerateEntries(ctx, txn)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Check wallet account code format
	walletAccountCode := entries[0].Metadata["account_code"].(string)
	expectedWalletCode := "wallet." + walletID.String() + ".BTC"
	assert.Equal(t, expectedWalletCode, walletAccountCode)

	// Check income account code format
	incomeAccountCode := entries[1].Metadata["account_code"].(string)
	assert.Equal(t, "income.BTC", incomeAccountCode)
}
