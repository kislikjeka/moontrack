//go:build integration

package ledger_test

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Concurrent Access Tests for Ledger Service
// These tests verify that the ledger handles concurrent operations correctly
// using row-level locking (SELECT FOR UPDATE) to prevent race conditions

// TestLedgerService_ConcurrentWithdrawals_NoDoubleSpend tests that concurrent withdrawals
// don't result in double-spending (withdrawing more than available)
func TestLedgerService_ConcurrentWithdrawals_NoDoubleSpend(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	// Register income handler for initial deposit
	incomeHandler := newTestHandler()
	require.NoError(t, registry.Register(incomeHandler))

	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Setup: Deposit 100 BTC
	_, err := svc.RecordTransaction(
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

	// Create registry with outcome handler
	outcomeRegistry := ledger.NewRegistry()
	outcomeHandler := &testOutcomeHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeManualOutcome),
		walletID:    walletID,
	}
	require.NoError(t, outcomeRegistry.Register(outcomeHandler))

	outcomeSvc := ledger.NewService(repo, outcomeRegistry)

	// Run 10 concurrent withdrawals of 50 BTC each
	// Only 2 should succeed (100/50 = 2), 8 should fail with insufficient balance
	numGoroutines := 10
	withdrawAmount := "50"

	var wg sync.WaitGroup
	var successCount int32
	var failCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
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
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Verify results
	t.Logf("Concurrent withdrawals: %d succeeded, %d failed", successCount, failCount)

	// At most 2 should succeed (100 / 50 = 2)
	assert.LessOrEqual(t, int(successCount), 2, "At most 2 withdrawals of 50 should succeed from 100 balance")

	// Get final balance and verify it's non-negative
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	assert.GreaterOrEqual(t, balance.Balance.Cmp(big.NewInt(0)), 0,
		"Final balance should be non-negative, got: %s", balance.Balance.String())

	// Verify expected balance based on successful withdrawals
	expectedBalance := 100 - (int(successCount) * 50)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(int64(expectedBalance))),
		"Final balance should be %d, got %s", expectedBalance, balance.Balance.String())
}

// TestLedgerService_ConcurrentDeposits_CorrectTotal tests that concurrent deposits
// result in the correct total balance
// Note: This test creates the account first to avoid race conditions in account creation.
// Account creation race is a separate issue from balance update race.
func TestLedgerService_ConcurrentDeposits_CorrectTotal(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// First, create the account by making an initial deposit (avoids account creation race)
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "0", // Zero deposit just to create account
		},
	)
	require.NoError(t, err)

	// Run 10 concurrent deposits of 10 BTC each
	numGoroutines := 10
	depositAmount := "10"

	var wg sync.WaitGroup
	var successCount int32
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
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

	// All deposits should succeed
	for err := range errors {
		t.Errorf("Deposit failed: %v", err)
	}
	assert.Equal(t, int32(numGoroutines), successCount, "All deposits should succeed")

	// Verify final balance: 10 * 10 = 100
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	expectedBalance := big.NewInt(int64(numGoroutines * 10))
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance),
		"Final balance should be %s, got %s", expectedBalance.String(), balance.Balance.String())

	// Reconcile to ensure entries match balance
	err = svc.ReconcileBalance(ctx, account.ID, "BTC")
	assert.NoError(t, err, "Reconciliation should pass after concurrent deposits")
}

// TestLedgerService_ConcurrentAccountCreation_NoDuplicates tests that concurrent
// account creation doesn't result in duplicate accounts
func TestLedgerService_ConcurrentAccountCreation_NoDuplicates(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Run 5 concurrent transactions for the same account (should create account once)
	numGoroutines := 5

	var wg sync.WaitGroup
	var successCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
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
		}(i)
	}

	wg.Wait()

	// At least some should succeed
	assert.Greater(t, int(successCount), 0, "At least some transactions should succeed")

	// Verify only one account exists for this wallet/asset
	accountCode := "wallet." + walletID.String() + ".ETH"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)
	require.NotNil(t, account)

	// Verify balance matches number of successful transactions
	balance, err := svc.GetAccountBalance(ctx, account.ID, "ETH")
	require.NoError(t, err)

	expectedBalance := big.NewInt(int64(successCount) * 100)
	assert.Equal(t, 0, balance.Balance.Cmp(expectedBalance),
		"Balance should be %s (100 * %d successful txns), got %s",
		expectedBalance.String(), successCount, balance.Balance.String())
}

// TestLedgerService_ConcurrentMixedOperations tests concurrent deposits and withdrawals
func TestLedgerService_ConcurrentMixedOperations(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)

	// Setup registries
	incomeRegistry := ledger.NewRegistry()
	incomeHandler := newTestHandler()
	require.NoError(t, incomeRegistry.Register(incomeHandler))
	incomeSvc := ledger.NewService(repo, incomeRegistry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Initial deposit of 1000
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

	outcomeRegistry := ledger.NewRegistry()
	outcomeHandler := &testOutcomeHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeManualOutcome),
		walletID:    walletID,
	}
	require.NoError(t, outcomeRegistry.Register(outcomeHandler))
	outcomeSvc := ledger.NewService(repo, outcomeRegistry)

	// Run concurrent deposits (+10) and withdrawals (-5)
	numDeposits := 10
	numWithdrawals := 10

	var wg sync.WaitGroup
	var depositSuccess, withdrawSuccess int32

	// Deposits
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

	// Withdrawals
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

	// Verify final balance is consistent
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := incomeSvc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)

	// Expected: 1000 + (depositSuccess * 10) - (withdrawSuccess * 5)
	expectedBalance := 1000 + (int64(depositSuccess) * 10) - (int64(withdrawSuccess) * 5)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(expectedBalance)),
		"Final balance should be %d, got %s", expectedBalance, balance.Balance.String())

	// Balance should never be negative
	assert.GreaterOrEqual(t, balance.Balance.Cmp(big.NewInt(0)), 0,
		"Balance should never be negative")

	// Reconciliation should pass
	err = incomeSvc.ReconcileBalance(ctx, account.ID, "BTC")
	assert.NoError(t, err, "Reconciliation should pass after mixed operations")
}
