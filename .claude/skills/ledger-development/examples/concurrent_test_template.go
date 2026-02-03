//go:build integration

// Package example provides test templates for ledger concurrent access testing.
// Copy and adapt this template when testing concurrent operations on the ledger.
package example

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/testutil"
)

// testDB is the shared test database instance
var testDB *testutil.TestDB

// =============================================================================
// Test Setup (put in testmain_test.go)
// =============================================================================

/*
func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	testDB, err = testutil.NewTestDB(ctx)
	if err != nil {
		panic(err)
	}

	code := m.Run()

	testDB.Close(ctx)
	os.Exit(code)
}
*/

// =============================================================================
// Concurrent Withdrawal Test - Double-Spend Prevention
// =============================================================================

// TestLedgerService_ConcurrentWithdrawals_NoDoubleSpend tests that concurrent
// withdrawals don't result in double-spending (withdrawing more than available).
// This is the most critical concurrent test for financial systems.
func TestLedgerService_ConcurrentWithdrawals_NoDoubleSpend(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)

	// Setup: Create user and wallet
	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// Setup: Deposit initial balance (100 units)
	incomeSvc := setupIncomeService(t, repo)
	_, err := incomeSvc.RecordTransaction(
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

	// Setup: Create outcome service
	outcomeSvc := setupOutcomeService(t, repo, walletID)

	// Test: Run 10 concurrent withdrawals of 50 each
	// Expected: Only 2 should succeed (100 / 50 = 2)
	numGoroutines := 10
	withdrawAmount := "50"

	var wg sync.WaitGroup
	var successCount int32
	var failCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			_, err := outcomeSvc.RecordTransaction(
				ctx,
				ledger.TxTypeManualOutcome,
				"manual",
				nil,
				time.Now().Add(-time.Hour),
				map[string]interface{}{
					"wallet_id": walletID.String(),
					"asset_id":  "BTC",
					"amount":    withdrawAmount,
				},
			)

			if err != nil {
				atomic.AddInt32(&failCount, 1)
				t.Logf("Goroutine %d: withdrawal failed - %v", goroutineID, err)
			} else {
				atomic.AddInt32(&successCount, 1)
				t.Logf("Goroutine %d: withdrawal succeeded", goroutineID)
			}
		}(i)
	}

	wg.Wait()

	// Verify: At most 2 should succeed
	t.Logf("Results: %d succeeded, %d failed", successCount, failCount)
	assert.LessOrEqual(t, int(successCount), 2,
		"At most 2 withdrawals of 50 should succeed from 100 balance")

	// Verify: Final balance is non-negative
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := incomeSvc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	assert.GreaterOrEqual(t, balance.Balance.Cmp(big.NewInt(0)), 0,
		"Final balance must be non-negative, got: %s", balance.Balance.String())

	// Verify: Balance is correct based on successful withdrawals
	expectedBalance := 100 - (int(successCount) * 50)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(int64(expectedBalance))),
		"Final balance should be %d, got %s", expectedBalance, balance.Balance.String())
}

// =============================================================================
// Concurrent Deposit Test - Correct Totals
// =============================================================================

// TestLedgerService_ConcurrentDeposits_CorrectTotal tests that concurrent
// deposits result in the correct total balance (no lost updates).
func TestLedgerService_ConcurrentDeposits_CorrectTotal(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	svc := setupIncomeService(t, repo)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// First, create the account by making an initial deposit
	// This avoids race conditions in account creation (separate concern)
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "0", // Zero deposit to create account
		},
	)
	require.NoError(t, err)

	// Test: Run 10 concurrent deposits of 10 each
	numGoroutines := 10
	depositAmount := "10"

	var wg sync.WaitGroup
	var successCount int32
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			_, err := svc.RecordTransaction(
				ctx,
				ledger.TxTypeManualIncome,
				"manual",
				nil,
				time.Now().Add(-time.Hour),
				map[string]interface{}{
					"wallet_id": walletID.String(),
					"asset_id":  "BTC",
					"amount":    depositAmount,
				},
			)

			if err != nil {
				errors <- err
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Verify: All deposits should succeed
	for err := range errors {
		t.Errorf("Deposit failed: %v", err)
	}
	assert.Equal(t, int32(numGoroutines), successCount, "All deposits should succeed")

	// Verify: Final balance equals sum of all deposits
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	expectedBalance := big.NewInt(int64(numGoroutines * 10))
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance),
		"Final balance should be %s, got %s", expectedBalance.String(), balance.Balance.String())

	// Verify: Reconciliation passes
	err = svc.ReconcileBalance(ctx, account.ID, "BTC")
	assert.NoError(t, err, "Reconciliation should pass after concurrent deposits")
}

// =============================================================================
// Concurrent Mixed Operations Test
// =============================================================================

// TestLedgerService_ConcurrentMixedOperations tests concurrent deposits and
// withdrawals together to verify system stability under mixed load.
func TestLedgerService_ConcurrentMixedOperations(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// Setup: Initial balance of 1000
	incomeSvc := setupIncomeService(t, repo)
	_, err := incomeSvc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "1000",
		},
	)
	require.NoError(t, err)

	outcomeSvc := setupOutcomeService(t, repo, walletID)

	// Test: Run concurrent deposits (+10) and withdrawals (-5)
	numDeposits := 10
	numWithdrawals := 10

	var wg sync.WaitGroup
	var depositSuccess, withdrawSuccess int32

	// Start deposits
	for i := 0; i < numDeposits; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := incomeSvc.RecordTransaction(
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
			if err == nil {
				atomic.AddInt32(&depositSuccess, 1)
			}
		}()
	}

	// Start withdrawals
	for i := 0; i < numWithdrawals; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := outcomeSvc.RecordTransaction(
				ctx,
				ledger.TxTypeManualOutcome,
				"manual",
				nil,
				time.Now().Add(-time.Hour),
				map[string]interface{}{
					"wallet_id": walletID.String(),
					"asset_id":  "BTC",
					"amount":    "5",
				},
			)
			if err == nil {
				atomic.AddInt32(&withdrawSuccess, 1)
			}
		}()
	}

	wg.Wait()

	t.Logf("Mixed operations: %d deposits succeeded, %d withdrawals succeeded",
		depositSuccess, withdrawSuccess)

	// Verify: Final balance is consistent
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := incomeSvc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	// Expected: 1000 + (depositSuccess * 10) - (withdrawSuccess * 5)
	expectedBalance := 1000 + (int64(depositSuccess) * 10) - (int64(withdrawSuccess) * 5)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(expectedBalance)),
		"Final balance should be %d, got %s", expectedBalance, balance.Balance.String())

	// Verify: Balance is never negative
	assert.GreaterOrEqual(t, balance.Balance.Cmp(big.NewInt(0)), 0,
		"Balance should never be negative")

	// Verify: Reconciliation passes
	err = incomeSvc.ReconcileBalance(ctx, account.ID, "BTC")
	assert.NoError(t, err, "Reconciliation should pass after mixed operations")
}

// =============================================================================
// Concurrent Account Creation Test
// =============================================================================

// TestLedgerService_ConcurrentAccountCreation_NoDuplicates tests that
// concurrent account creation doesn't result in duplicate accounts.
func TestLedgerService_ConcurrentAccountCreation_NoDuplicates(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	svc := setupIncomeService(t, repo)

	userID := createTestUser(t, ctx)
	walletID := createTestWallet(t, ctx, userID)

	// Test: Run 5 concurrent transactions that all try to create the same account
	numGoroutines := 5

	var wg sync.WaitGroup
	var successCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, err := svc.RecordTransaction(
				ctx,
				ledger.TxTypeManualIncome,
				"manual",
				nil,
				time.Now().Add(-time.Hour),
				map[string]interface{}{
					"wallet_id": walletID.String(),
					"asset_id":  "ETH", // Same asset for all
					"amount":    "100",
				},
			)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}

	wg.Wait()

	// Verify: At least some should succeed
	assert.Greater(t, int(successCount), 0, "At least some transactions should succeed")

	// Verify: Only ONE account exists for this wallet/asset
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)
	require.NotNil(t, account)

	// Verify: Balance matches number of successful transactions
	balance, err := svc.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)

	expectedBalance := big.NewInt(int64(successCount) * 100)
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance),
		"Balance should be %s (100 * %d successful txns), got %s",
		expectedBalance.String(), successCount, balance.Balance.String())
}

// =============================================================================
// Helper Functions (implement based on your actual code)
// =============================================================================

func createTestUser(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	// TODO: Implement based on your test setup
	// Example:
	// userID := uuid.New()
	// _, err := testDB.Pool.Exec(ctx, `INSERT INTO users ...`, userID)
	// require.NoError(t, err)
	// return userID
	return uuid.New()
}

func createTestWallet(t *testing.T, ctx context.Context, userID uuid.UUID) uuid.UUID {
	t.Helper()
	// TODO: Implement based on your test setup
	// Example:
	// walletID := uuid.New()
	// _, err := testDB.Pool.Exec(ctx, `INSERT INTO wallets ...`, walletID, userID)
	// require.NoError(t, err)
	// return walletID
	_ = userID
	return uuid.New()
}

func setupIncomeService(t *testing.T, repo *postgres.LedgerRepository) *ledger.Service {
	t.Helper()
	// TODO: Implement based on your actual service setup
	// Example:
	// registry := ledger.NewRegistry()
	// handler := manual.NewManualIncomeHandler(priceService, walletRepo)
	// require.NoError(t, registry.Register(handler))
	// return ledger.NewService(repo, registry)
	_ = repo
	return nil
}

func setupOutcomeService(t *testing.T, repo *postgres.LedgerRepository, walletID uuid.UUID) *ledger.Service {
	t.Helper()
	// TODO: Implement based on your actual service setup
	// Example:
	// registry := ledger.NewRegistry()
	// handler := manual.NewManualOutcomeHandler(priceService, walletRepo, balanceGetter)
	// require.NoError(t, registry.Register(handler))
	// return ledger.NewService(repo, registry)
	_ = repo
	_ = walletID
	return nil
}
