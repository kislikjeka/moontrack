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

// TestTransactionProcessingPerformanceSC002 verifies SC-002 requirement:
// Balance updates must complete within 2 seconds (T117A)
func TestTransactionProcessingPerformanceSC002(t *testing.T) {
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
	createTestUser(t, db, userID, "perf@example.com")
	wallet, err := walletService.Create(ctx, userID, "Performance Test Wallet", "ethereum", nil)
	require.NoError(t, err)

	tests := []struct {
		name           string
		transactionType string
		amount         *big.Int
		setupBalance   *big.Int // For outcome tests, need existing balance
	}{
		{
			name:            "income_transaction_small_amount",
			transactionType: "income",
			amount:          big.NewInt(1000000000000000), // 0.001 ETH
		},
		{
			name:            "income_transaction_large_amount",
			transactionType: "income",
			amount:          big.NewInt(1000000000000000000000), // 1000 ETH
		},
		{
			name:            "outcome_transaction",
			transactionType: "outcome",
			amount:          big.NewInt(500000000000000000), // 0.5 ETH
			setupBalance:    big.NewInt(2000000000000000000), // 2 ETH initial
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usdRate := big.NewInt(200000000000) // $2000 * 10^8

			// Setup initial balance if needed
			if tt.setupBalance != nil {
				incomeHandler := setupIncomeHandler(ledgerService, priceService)
				_, err := incomeHandler.Execute(ctx, userID, &ManualIncomeTransaction{
					WalletID:    wallet.ID,
					AssetID:     "ETH",
					Amount:      tt.setupBalance,
					USDRate:     usdRate,
					OccurredAt:  time.Now(),
					PriceSource: "manual",
				})
				require.NoError(t, err)
			}

			// Get initial balance
			initialBalance, err := ledgerRepo.GetBalance(ctx, wallet.ID, "ETH")
			require.NoError(t, err)

			// Measure transaction execution time
			start := time.Now()

			var txID uuid.UUID
			if tt.transactionType == "income" {
				handler := setupIncomeHandler(ledgerService, priceService)
				txID, err = handler.Execute(ctx, userID, &ManualIncomeTransaction{
					WalletID:    wallet.ID,
					AssetID:     "ETH",
					Amount:      tt.amount,
					USDRate:     usdRate,
					OccurredAt:  time.Now(),
					PriceSource: "manual",
				})
			} else {
				handler := setupOutcomeHandler(ledgerService, ledgerRepo, priceService)
				txID, err = handler.Execute(ctx, userID, &ManualOutcomeTransaction{
					WalletID:    wallet.ID,
					AssetID:     "ETH",
					Amount:      tt.amount,
					USDRate:     usdRate,
					OccurredAt:  time.Now(),
					PriceSource: "manual",
				})
			}

			duration := time.Since(start)
			require.NoError(t, err)
			assert.NotEqual(t, uuid.Nil, txID)

			// VERIFY SC-002: Transaction processing must complete within 2 seconds
			maxDuration := 2 * time.Second
			assert.Less(t, duration, maxDuration,
				"SC-002 VIOLATION: Transaction processing took %v, must be under %v",
				duration, maxDuration)

			// Verify balance is immediately queryable
			queryStart := time.Now()
			newBalance, err := ledgerRepo.GetBalance(ctx, wallet.ID, "ETH")
			queryDuration := time.Since(queryStart)
			require.NoError(t, err)

			// Balance query should also be fast (<100ms)
			assert.Less(t, queryDuration, 100*time.Millisecond,
				"Balance query took %v, should be under 100ms", queryDuration)

			// Verify balance correctness
			var expectedBalance *big.Int
			if tt.transactionType == "income" {
				expectedBalance = big.NewInt(0).Add(initialBalance, tt.amount)
			} else {
				expectedBalance = big.NewInt(0).Sub(initialBalance, tt.amount)
			}
			assert.Equal(t, expectedBalance.String(), newBalance.String(),
				"Balance should be correctly updated")

			// Log performance metrics
			t.Logf("Transaction type: %s, Amount: %s wei", tt.transactionType, tt.amount.String())
			t.Logf("  - Transaction execution: %v (requirement: <%v)", duration, maxDuration)
			t.Logf("  - Balance query: %v", queryDuration)
			t.Logf("  - Total time: %v", duration+queryDuration)
		})
	}
}

// TestConcurrentTransactionPerformance tests multiple transactions
// being processed concurrently (stress test)
func TestConcurrentTransactionPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent performance test in short mode")
	}

	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Initialize services
	ledgerRepo := setupLedgerRepository(db)
	ledgerService := setupLedgerService(ledgerRepo)
	walletService := setupWalletService(db)
	priceService := setupMockPriceService()

	// Create test user and multiple wallets
	userID := uuid.New()
	createTestUser(t, db, userID, "concurrent@example.com")

	numWallets := 10
	wallets := make([]uuid.UUID, numWallets)
	for i := 0; i < numWallets; i++ {
		wallet, err := walletService.Create(ctx, userID,
			fmt.Sprintf("Wallet %d", i), "ethereum", nil)
		require.NoError(t, err)
		wallets[i] = wallet.ID
	}

	// Execute 10 concurrent transactions
	start := time.Now()
	errChan := make(chan error, numWallets)
	doneChan := make(chan bool, numWallets)

	handler := setupIncomeHandler(ledgerService, priceService)
	amount := big.NewInt(1000000000000000000) // 1 ETH
	usdRate := big.NewInt(200000000000)

	for i := 0; i < numWallets; i++ {
		go func(walletID uuid.UUID) {
			_, err := handler.Execute(ctx, userID, &ManualIncomeTransaction{
				WalletID:    walletID,
				AssetID:     "ETH",
				Amount:      amount,
				USDRate:     usdRate,
				OccurredAt:  time.Now(),
				PriceSource: "manual",
			})
			if err != nil {
				errChan <- err
			}
			doneChan <- true
		}(wallets[i])
	}

	// Wait for all transactions to complete
	for i := 0; i < numWallets; i++ {
		select {
		case err := <-errChan:
			t.Fatalf("Concurrent transaction failed: %v", err)
		case <-doneChan:
			// Transaction completed successfully
		case <-time.After(5 * time.Second):
			t.Fatal("Concurrent transactions timed out after 5 seconds")
		}
	}

	duration := time.Since(start)
	t.Logf("Processed %d concurrent transactions in %v (avg: %v per transaction)",
		numWallets, duration, duration/time.Duration(numWallets))

	// Verify all balances updated correctly
	for i, walletID := range wallets {
		balance, err := ledgerRepo.GetBalance(ctx, walletID, "ETH")
		require.NoError(t, err, "Wallet %d balance check failed", i)
		assert.Equal(t, amount.String(), balance.String(),
			"Wallet %d should have correct balance", i)
	}
}

// TestAPIEndToEndPerformance tests the full API request cycle including
// HTTP parsing, validation, transaction processing, and response generation
func TestAPIEndToEndPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end performance test in short mode")
	}

	// TODO: Implement full HTTP API performance test
	// This should test POST /transactions endpoint with actual HTTP requests
	// and verify the total response time is under 200ms (constitution requirement)

	t.Skip("API performance test requires HTTP server setup - implement after API handlers complete")
}
