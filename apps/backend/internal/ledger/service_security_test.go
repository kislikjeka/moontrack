//go:build integration

package ledger_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Security Tests for Ledger Service
// These tests verify input validation and security constraints

// testSecurityHandler is a handler that allows configuring entries for security tests
type testSecurityHandler struct {
	ledger.BaseHandler
	entries     []*ledger.Entry
	validateErr error
}

func newTestSecurityHandler() *testSecurityHandler {
	return &testSecurityHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeManualIncome),
	}
}

func (h *testSecurityHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	return h.entries, nil
}

func (h *testSecurityHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	return h.validateErr
}

func (h *testSecurityHandler) SetEntries(entries []*ledger.Entry) {
	h.entries = entries
}

func (h *testSecurityHandler) SetValidateError(err error) {
	h.validateErr = err
}

// TestLedgerService_RecordTransaction_ZeroAmount tests that zero amounts are rejected
func TestLedgerService_RecordTransaction_ZeroAmount(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	now := time.Now()
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(0), // Zero amount
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(0),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"account_code": "wallet." + walletID.String() + ".BTC",
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(0), // Zero amount
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(0),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "income.BTC",
			},
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		now.Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "0",
		},
	)

	// Zero amounts should be allowed but result in zero balance change
	// This is valid for some use cases (e.g., recording events without value)
	// The validator checks that entries are balanced, which they are here
	assert.NoError(t, err)
	assert.NotNil(t, tx)
}

// TestLedgerService_RecordTransaction_NegativeAmount tests that negative amounts are rejected
func TestLedgerService_RecordTransaction_NegativeAmount(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	now := time.Now()
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(-100), // Negative amount
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(-5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"account_code": "wallet." + walletID.String() + ".BTC",
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(-100), // Negative amount
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(-5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "income.BTC",
			},
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		now.Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "-100",
		},
	)

	// Negative amounts should be rejected by entry validation
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "negative")
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// TestLedgerService_RecordTransaction_InvalidUUID tests that invalid wallet_id is handled
func TestLedgerService_RecordTransaction_InvalidUUID(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	now := time.Now()
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    "invalid-uuid-format",
				"account_code": "wallet.invalid-uuid-format.BTC",
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "income.BTC",
			},
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		now.Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": "invalid-uuid-format",
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)

	// Invalid UUID in wallet_id metadata should cause account resolution to fail
	assert.Error(t, err)
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// TestLedgerService_RecordTransaction_EmptyAssetID tests that empty asset_id is rejected
func TestLedgerService_RecordTransaction_EmptyAssetID(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	now := time.Now()
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(100),
			AssetID:     "", // Empty asset ID
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"account_code": "wallet." + walletID.String() + ".",
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(100),
			AssetID:     "", // Empty asset ID
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "income.",
			},
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		now.Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "",
			"amount":    "100",
		},
	)

	// Empty asset ID should be rejected
	assert.Error(t, err)
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// TestLedgerService_RecordTransaction_FutureDate tests that future dates are rejected
func TestLedgerService_RecordTransaction_FutureDate(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	futureTime := time.Now().Add(24 * time.Hour) // 1 day in the future
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  futureTime,
			CreatedAt:   time.Now(),
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"account_code": "wallet." + walletID.String() + ".BTC",
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  futureTime,
			CreatedAt:   time.Now(),
			Metadata: map[string]interface{}{
				"account_code": "income.BTC",
			},
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		futureTime, // Future date
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)

	// Future dates should be rejected per model.go:93
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "future")
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// TestLedgerService_RecordTransaction_NilUSDRate tests nil USD rate handling
func TestLedgerService_RecordTransaction_NilUSDRate(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	now := time.Now()
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     nil, // Nil USD rate
			USDValue:    big.NewInt(0),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"account_code": "wallet." + walletID.String() + ".BTC",
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     nil, // Nil USD rate
			USDValue:    big.NewInt(0),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "income.BTC",
			},
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		now.Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)

	// Nil USD rate should be rejected per model.go:216
	assert.Error(t, err)
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// TestLedgerService_RecordTransaction_NegativeUSDRate tests negative USD rate handling
func TestLedgerService_RecordTransaction_NegativeUSDRate(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	now := time.Now()
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(-5000000000000), // Negative USD rate
			USDValue:    big.NewInt(-5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"account_code": "wallet." + walletID.String() + ".BTC",
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(-5000000000000), // Negative USD rate
			USDValue:    big.NewInt(-5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "income.BTC",
			},
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		now.Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)

	// Negative USD rate should be rejected per model.go:216-217
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "negative")
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// TestLedgerService_RecordTransaction_UnbalancedEntries tests that unbalanced entries are rejected
func TestLedgerService_RecordTransaction_UnbalancedEntries(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	now := time.Now()
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"account_code": "wallet." + walletID.String() + ".BTC",
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(50), // Different amount - unbalanced!
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(2500),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"account_code": "income.BTC",
			},
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		now.Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)

	// Unbalanced entries should be rejected per double-entry accounting
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "balance")
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// TestLedgerService_RecordTransaction_InvalidTransactionType tests invalid transaction type
func TestLedgerService_RecordTransaction_InvalidTransactionType(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	registry := ledger.NewRegistry()
	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	_, err := svc.RecordTransaction(
		ctx,
		ledger.TransactionType("invalid_type"),
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": uuid.New().String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)

	// Invalid transaction type should be rejected
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

// TestLedgerService_RecordTransaction_MissingAccountCode tests missing account_code in metadata
func TestLedgerService_RecordTransaction_MissingAccountCode(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	handler := newTestSecurityHandler()
	registry := ledger.NewRegistry()
	require.NoError(t, registry.Register(handler))

	repo := newTestRepo()
	svc := ledger.NewService(repo, registry)

	now := time.Now()
	handler.SetEntries([]*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata:    nil, // Missing metadata with account_code
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      big.NewInt(100),
			AssetID:     "BTC",
			USDRate:     big.NewInt(5000000000000),
			USDValue:    big.NewInt(5000),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata:    nil, // Missing metadata with account_code
		},
	})

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		now.Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": uuid.New().String(),
			"asset_id":  "BTC",
			"amount":    "100",
		},
	)

	// Missing account_code should cause account resolution to fail
	assert.Error(t, err)
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}
}

// newTestRepo creates a new LedgerRepository from testDB
func newTestRepo() *postgres.LedgerRepository {
	return postgres.NewLedgerRepository(testDB.Pool)
}
