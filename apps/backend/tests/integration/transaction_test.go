package integration

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncomeTransactionIncreasesWalletBalance verifies that income transactions
// correctly increase wallet balance through the ledger system (T116)
func TestIncomeTransactionIncreasesWalletBalance(t *testing.T) {
	// Setup test database and services
	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Initialize services
	ledgerRepo := setupLedgerRepository(db)
	ledgerService := setupLedgerService(ledgerRepo)
	walletService := setupWalletService(db)
	priceService := setupMockPriceService()

	// Create test user
	userID := uuid.New()
	createTestUser(t, db, userID, "test@example.com")

	// Create test wallet
	wallet, err := walletService.Create(ctx, userID, "Test Wallet", "ethereum", nil)
	require.NoError(t, err)

	// Get initial balance (should be 0)
	initialBalance, err := ledgerRepo.GetBalance(ctx, wallet.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, "0", initialBalance.String())

	// Create manual income transaction
	amount := big.NewInt(1000000000000000000) // 1 ETH in wei
	usdRate := big.NewInt(200000000000)        // $2000 * 10^8

	incomeHandler := setupIncomeHandler(ledgerService, priceService)
	transaction := &ManualIncomeTransaction{
		WalletID:    wallet.ID,
		AssetID:     "ETH",
		Amount:      amount,
		USDRate:     usdRate,
		OccurredAt:  time.Now(),
		Notes:       "Test deposit",
		PriceSource: "manual",
	}

	// Execute transaction
	txID, err := incomeHandler.Execute(ctx, userID, transaction)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, txID)

	// Verify balance increased
	newBalance, err := ledgerRepo.GetBalance(ctx, wallet.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, amount.String(), newBalance.String(), "balance should equal deposited amount")

	// Verify ledger entries balance
	entries, err := ledgerRepo.GetEntriesByTransaction(ctx, txID)
	require.NoError(t, err)
	require.Len(t, entries, 2, "should have 2 ledger entries (debit + credit)")

	// Verify balance invariant: SUM(debit) = SUM(credit)
	debitSum := big.NewInt(0)
	creditSum := big.NewInt(0)
	for _, entry := range entries {
		if entry.DebitCredit == "DEBIT" {
			debitSum.Add(debitSum, entry.Amount)
		} else {
			creditSum.Add(creditSum, entry.Amount)
		}
	}
	assert.Equal(t, 0, debitSum.Cmp(creditSum), "ledger must balance: SUM(debit) = SUM(credit)")
}

// TestOutcomeTransactionDecreasesWalletBalance verifies that outcome transactions
// correctly decrease wallet balance and reject insufficient balance (T117)
func TestOutcomeTransactionDecreasesWalletBalance(t *testing.T) {
	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Initialize services
	ledgerRepo := setupLedgerRepository(db)
	ledgerService := setupLedgerService(ledgerRepo)
	walletService := setupWalletService(db)
	priceService := setupMockPriceService()

	// Create test user
	userID := uuid.New()
	createTestUser(t, db, userID, "test@example.com")

	// Create test wallet with initial balance
	wallet, err := walletService.Create(ctx, userID, "Test Wallet", "ethereum", nil)
	require.NoError(t, err)

	// Add initial balance via income transaction
	initialAmount := big.NewInt(2000000000000000000) // 2 ETH
	usdRate := big.NewInt(200000000000)                // $2000 * 10^8

	incomeHandler := setupIncomeHandler(ledgerService, priceService)
	_, err = incomeHandler.Execute(ctx, userID, &ManualIncomeTransaction{
		WalletID:    wallet.ID,
		AssetID:     "ETH",
		Amount:      initialAmount,
		USDRate:     usdRate,
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	})
	require.NoError(t, err)

	// Test 1: Successful withdrawal (sufficient balance)
	t.Run("successful_withdrawal", func(t *testing.T) {
		withdrawAmount := big.NewInt(1000000000000000000) // 1 ETH

		outcomeHandler := setupOutcomeHandler(ledgerService, ledgerRepo, priceService)
		txID, err := outcomeHandler.Execute(ctx, userID, &ManualOutcomeTransaction{
			WalletID:    wallet.ID,
			AssetID:     "ETH",
			Amount:      withdrawAmount,
			USDRate:     usdRate,
			OccurredAt:  time.Now(),
			Notes:       "Test withdrawal",
			PriceSource: "manual",
		})
		require.NoError(t, err)

		// Verify balance decreased
		newBalance, err := ledgerRepo.GetBalance(ctx, wallet.ID, "ETH")
		require.NoError(t, err)
		expectedBalance := big.NewInt(0).Sub(initialAmount, withdrawAmount)
		assert.Equal(t, expectedBalance.String(), newBalance.String())

		// Verify ledger entries balance
		entries, err := ledgerRepo.GetEntriesByTransaction(ctx, txID)
		require.NoError(t, err)
		require.Len(t, entries, 2)

		debitSum := big.NewInt(0)
		creditSum := big.NewInt(0)
		for _, entry := range entries {
			if entry.DebitCredit == "DEBIT" {
				debitSum.Add(debitSum, entry.Amount)
			} else {
				creditSum.Add(creditSum, entry.Amount)
			}
		}
		assert.Equal(t, 0, debitSum.Cmp(creditSum), "ledger must balance")
	})

	// Test 2: Reject insufficient balance
	t.Run("reject_insufficient_balance", func(t *testing.T) {
		excessiveAmount := big.NewInt(5000000000000000000) // 5 ETH (more than balance)

		outcomeHandler := setupOutcomeHandler(ledgerService, ledgerRepo, priceService)
		_, err := outcomeHandler.Execute(ctx, userID, &ManualOutcomeTransaction{
			WalletID:    wallet.ID,
			AssetID:     "ETH",
			Amount:      excessiveAmount,
			USDRate:     usdRate,
			OccurredAt:  time.Now(),
			PriceSource: "manual",
		})
		require.Error(t, err, "should reject withdrawal exceeding balance")
		assert.Contains(t, err.Error(), "insufficient balance", "error should mention insufficient balance")
	})
}

// TestTransactionProcessingPerformance verifies that balance updates
// meet the SC-002 requirement (<2 seconds) (T117A)
func TestTransactionProcessingPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Initialize services
	ledgerRepo := setupLedgerRepository(db)
	ledgerService := setupLedgerService(ledgerRepo)
	walletService := setupWalletService(db)
	priceService := setupMockPriceService()

	// Create test user and wallet
	userID := uuid.New()
	createTestUser(t, db, userID, "test@example.com")
	wallet, err := walletService.Create(ctx, userID, "Perf Test Wallet", "ethereum", nil)
	require.NoError(t, err)

	// Test transaction processing time
	amount := big.NewInt(1000000000000000000)
	usdRate := big.NewInt(200000000000)

	incomeHandler := setupIncomeHandler(ledgerService, priceService)
	transaction := &ManualIncomeTransaction{
		WalletID:    wallet.ID,
		AssetID:     "ETH",
		Amount:      amount,
		USDRate:     usdRate,
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	}

	// Measure execution time
	start := time.Now()
	txID, err := incomeHandler.Execute(ctx, userID, transaction)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, txID)

	// Verify performance requirement: <2 seconds (SC-002)
	maxDuration := 2 * time.Second
	assert.Less(t, duration, maxDuration,
		"transaction processing took %v, must be under %v (SC-002)",
		duration, maxDuration)

	// Log performance metric
	t.Logf("Transaction processing completed in %v (requirement: <%v)", duration, maxDuration)

	// Verify balance update is available immediately
	balance, err := ledgerRepo.GetBalance(ctx, wallet.ID, "ETH")
	require.NoError(t, err)
	assert.Equal(t, amount.String(), balance.String(), "balance should be immediately available")
}

// Helper functions for test setup

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	// TODO: Implement test database setup with pgx
	// This should create a test database connection and return cleanup function
	t.Helper()
	// Implementation depends on actual database setup
	return nil, func() {}
}

func setupLedgerRepository(db *sql.DB) LedgerRepository {
	// TODO: Initialize PostgreSQL ledger repository
	return nil
}

func setupLedgerService(repo LedgerRepository) *LedgerService {
	// TODO: Initialize ledger service with repository
	return nil
}

func setupWalletService(db *sql.DB) *WalletService {
	// TODO: Initialize wallet service
	return nil
}

func setupMockPriceService() PriceService {
	// TODO: Create mock price service that returns predefined prices
	return nil
}

func setupIncomeHandler(ledger *LedgerService, prices PriceService) *ManualIncomeHandler {
	// TODO: Initialize income transaction handler
	return nil
}

func setupOutcomeHandler(ledger *LedgerService, repo LedgerRepository, prices PriceService) *ManualOutcomeHandler {
	// TODO: Initialize outcome transaction handler
	return nil
}

func createTestUser(t *testing.T, db *sql.DB, userID uuid.UUID, email string) {
	// TODO: Insert test user into database
	t.Helper()
}

// Type stubs (replace with actual imports from internal packages)

type ManualIncomeTransaction struct {
	WalletID    uuid.UUID
	AssetID     string
	Amount      *big.Int
	USDRate     *big.Int
	OccurredAt  time.Time
	Notes       string
	PriceSource string
}

type ManualOutcomeTransaction struct {
	WalletID    uuid.UUID
	AssetID     string
	Amount      *big.Int
	USDRate     *big.Int
	OccurredAt  time.Time
	Notes       string
	PriceSource string
}

type LedgerRepository interface {
	GetBalance(ctx context.Context, walletID uuid.UUID, assetID string) (*big.Int, error)
	GetEntriesByTransaction(ctx context.Context, txID uuid.UUID) ([]LedgerEntry, error)
}

type LedgerEntry struct {
	ID          uuid.UUID
	DebitCredit string
	Amount      *big.Int
}

type LedgerService struct{}
type WalletService struct{}
type PriceService interface{}
type ManualIncomeHandler struct{}
type ManualOutcomeHandler struct{}
