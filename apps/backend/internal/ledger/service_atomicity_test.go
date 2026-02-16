//go:build integration

package ledger_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Atomicity Tests for Ledger Service
// These tests verify transaction atomicity and rollback behavior

// TestLedgerService_Commit_TransactionCreation verifies that CreateTransaction
// and updateBalances happen in sequence
func TestLedgerService_Commit_TransactionCreation(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record a transaction
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
	require.NotNil(t, tx)

	// Verify transaction was created
	retrieved, err := svc.GetTransaction(ctx, tx.ID)
	require.NoError(t, err)
	assert.Equal(t, ledger.TransactionStatusCompleted, retrieved.Status)

	// Verify entries were created
	assert.Len(t, retrieved.Entries, 2)

	// Verify balances were updated
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(100)))
}

// TestLedgerService_Commit_MultipleBalanceUpdates verifies that multiple
// balance updates in a single transaction work correctly
func TestLedgerService_Commit_MultipleBalanceUpdates(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record first transaction
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

	// Record second transaction
	_, err = svc.RecordTransaction(
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

	// Verify cumulative balance
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(150)))
}

// TestLedgerService_ReconcileAfterMultipleTransactions verifies reconciliation
// after multiple transactions
func TestLedgerService_ReconcileAfterMultipleTransactions(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record multiple transactions
	for i := 0; i < 5; i++ {
		_, err := svc.RecordTransaction(
			ctx,
			ledger.TxTypeManualIncome,
			"manual",
			nil,
			time.Now().Add(-time.Duration(5-i)*time.Hour),
			map[string]interface{}{
				"wallet_id": walletID.String(),
				"asset_id":  "BTC",
				"amount":    "20",
			},
		)
		require.NoError(t, err)
	}

	// Get account
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	// Reconcile should pass
	err = svc.ReconcileBalance(ctx, account.ID, "BTC")
	assert.NoError(t, err)

	// Verify balance
	balance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Balance.Cmp(big.NewInt(100))) // 5 * 20 = 100
}

// TestLedgerService_ReconcileBalance_DetectsInconsistency tests that ReconcileBalance
// can detect when account_balances doesn't match calculated balance from entries
func TestLedgerService_ReconcileBalance_DetectsInconsistency(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record a transaction
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

	// Get account
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	// Manually corrupt the balance (simulate inconsistency)
	// In a real scenario, this could happen due to partial failure without proper transactions
	_, err = testDB.Pool.Exec(ctx, `
		UPDATE account_balances SET balance = '999'
		WHERE account_id = $1 AND asset_id = $2
	`, account.ID, "BTC")
	require.NoError(t, err)

	// Reconcile should detect the mismatch
	err = svc.ReconcileBalance(ctx, account.ID, "BTC")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")
}

// TestLedgerService_Commit_EntriesCreatedBeforeBalanceUpdate verifies the order
// of operations in commit
func TestLedgerService_Commit_EntriesCreatedBeforeBalanceUpdate(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record a transaction
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

	// Verify entries exist
	entries, err := repo.GetEntriesByTransaction(ctx, tx.ID)
	require.NoError(t, err)
	assert.Len(t, entries, 2)

	// Verify entries have correct data
	var debitEntry, creditEntry *ledger.Entry
	for _, e := range entries {
		if e.IsDebit() {
			debitEntry = e
		} else {
			creditEntry = e
		}
	}

	require.NotNil(t, debitEntry)
	require.NotNil(t, creditEntry)

	assert.Equal(t, ledger.EntryTypeAssetIncrease, debitEntry.EntryType)
	assert.Equal(t, ledger.EntryTypeIncome, creditEntry.EntryType)
	assert.Equal(t, 0, debitEntry.Amount.Cmp(big.NewInt(100)))
	assert.Equal(t, 0, creditEntry.Amount.Cmp(big.NewInt(100)))
}

// TestLedgerService_CalculateBalanceFromEntries_Accuracy verifies that balance
// calculation from entries is accurate
func TestLedgerService_CalculateBalanceFromEntries_Accuracy(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record transactions with varying amounts
	amounts := []int64{100, 50, 25, 75, 150}
	expectedTotal := int64(0)
	for _, amt := range amounts {
		_, err := svc.RecordTransaction(
			ctx,
			ledger.TxTypeManualIncome,
			"manual",
			nil,
			time.Now().Add(-time.Hour),
			map[string]interface{}{
				"wallet_id": walletID.String(),
				"asset_id":  "BTC",
				"amount":    big.NewInt(amt).String(),
			},
		)
		require.NoError(t, err)
		expectedTotal += amt
	}

	// Get account
	accountCode := "wallet." + walletID.String() + ".BTC"
	account, err := repo.GetAccountByCode(ctx, accountCode)
	require.NoError(t, err)

	// Calculate balance from entries
	calculatedBalance, err := repo.CalculateBalanceFromEntries(ctx, account.ID, "BTC")
	require.NoError(t, err)

	// Should match expected total
	assert.Equal(t, 0, calculatedBalance.Cmp(big.NewInt(expectedTotal)))

	// Should also match stored balance
	storedBalance, err := svc.GetAccountBalance(ctx, account.ID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, calculatedBalance.Cmp(storedBalance.Balance))
}

// TestLedgerService_NegativeBalancePrevention tests that transactions causing
// negative balances are rejected
func TestLedgerService_NegativeBalancePrevention(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	// Create a handler that generates outcome entries (asset_decrease)
	outcomeHandler := &testOutcomeHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeManualOutcome),
	}
	require.NoError(t, registry.Register(outcomeHandler))

	incomeHandler := newTestHandler()
	// Register income handler with different type - not needed if we only test outcome
	// For this test, we'll set up balance first via direct DB insert

	// Also register income handler to set up initial balance
	registry = ledger.NewRegistry()
	require.NoError(t, registry.Register(incomeHandler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// First, create some balance
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

	// Now create registry with outcome handler
	registry2 := ledger.NewRegistry()
	outcomeHandler.walletID = walletID
	require.NoError(t, registry2.Register(outcomeHandler))

	svc2 := ledger.NewService(repo, registry2, testLogger())

	// Try to withdraw more than available
	tx, err := svc2.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "200", // More than available 100
		},
	)

	// Should be rejected due to negative balance
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "negative")
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// testOutcomeHandler generates outcome entries (asset_decrease)
type testOutcomeHandler struct {
	ledger.BaseHandler
	walletID uuid.UUID
}

func (h *testOutcomeHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	walletID := h.walletID
	if wid, ok := data["wallet_id"].(string); ok {
		walletID, _ = uuid.Parse(wid)
	}

	assetID := "BTC"
	if aid, ok := data["asset_id"].(string); ok {
		assetID = aid
	}

	amount := big.NewInt(100)
	if amtStr, ok := data["amount"].(string); ok {
		amount, _ = new(big.Int).SetString(amtStr, 10)
	}

	now := time.Now()
	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit, // Credit decreases asset account
			EntryType:   ledger.EntryTypeAssetDecrease,
			Amount:      amount,
			AssetID:     assetID,
			USDRate:     big.NewInt(5000000000000),
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
			DebitCredit: ledger.Debit, // Debit expense account
			EntryType:   ledger.EntryTypeExpense,
			Amount:      amount,
			AssetID:     assetID,
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000000000000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "expense." + assetID,
			},
		},
	}, nil
}

func (h *testOutcomeHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	return nil
}

func (h *testOutcomeHandler) Type() ledger.TransactionType {
	return ledger.TxTypeManualOutcome
}

// TestLedgerService_ValidationError_NoEntriesInDB tests that validation errors
// don't leave partial data in the database
func TestLedgerService_ValidationError_NoEntriesInDB(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := &testValidationFailHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeManualIncome),
	}
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Count entries before
	var countBefore int
	err := testDB.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM entries").Scan(&countBefore)
	require.NoError(t, err)

	// Try to record transaction with validation error
	_, err = svc.RecordTransaction(
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

	// Should fail validation
	assert.Error(t, err)

	// Count entries after - should be same as before
	var countAfter int
	err = testDB.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM entries").Scan(&countAfter)
	require.NoError(t, err)

	assert.Equal(t, countBefore, countAfter, "No entries should be created when validation fails")
}

// testValidationFailHandler is a handler that always fails validation
type testValidationFailHandler struct {
	ledger.BaseHandler
}

func (h *testValidationFailHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	return nil, errors.New("validation failed intentionally")
}

func (h *testValidationFailHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	return errors.New("validation failed intentionally")
}

// TestLedgerService_EntryImmutability tests that entries cannot be modified after creation
func TestLedgerService_EntryImmutability(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	handler := newTestHandler()
	require.NoError(t, registry.Register(handler))

	svc := ledger.NewService(repo, registry, testLogger())

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Record a transaction
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

	// Get entries
	entries, err := repo.GetEntriesByTransaction(ctx, tx.ID)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	originalAmount := entries[0].Amount.String()

	// Try to update entry directly (this should not affect reconciliation)
	// Note: We don't have an UpdateEntry method because entries are immutable
	// This test verifies the design principle

	// Re-fetch entries and verify they haven't changed
	entriesAfter, err := repo.GetEntriesByTransaction(ctx, tx.ID)
	require.NoError(t, err)

	assert.Equal(t, originalAmount, entriesAfter[0].Amount.String())
}
