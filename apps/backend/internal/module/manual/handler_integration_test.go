//go:build integration

package manual_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/manual"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
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

func setupTest(t *testing.T) (context.Context, *ledger.Service) {
	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))

	repo := postgres.NewLedgerRepository(testDB.Pool)
	walletRepo := postgres.NewWalletRepository(testDB.Pool)
	registry := ledger.NewRegistry()

	// Create mock registry service
	mockRegistry := &mockRegistryService{
		prices: map[string]*big.Int{
			"bitcoin":  big.NewInt(5000000000000), // $50,000 * 10^8
			"ethereum": big.NewInt(300000000000),  // $3,000 * 10^8
		},
	}

	// Register handlers
	incomeHandler := manual.NewManualIncomeHandler(mockRegistry, walletRepo)
	registry.Register(incomeHandler)

	outcomeHandler := manual.NewManualOutcomeHandler(mockRegistry, walletRepo, ledger.NewService(repo, registry))
	registry.Register(outcomeHandler)

	svc := ledger.NewService(repo, registry)
	return ctx, svc
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

// mockRegistryService implements manual.RegistryService for testing
type mockRegistryService struct {
	prices map[string]*big.Int
}

func (m *mockRegistryService) GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error) {
	if price, ok := m.prices[coinGeckoID]; ok {
		return price, nil
	}
	return big.NewInt(100000000), nil // Default $1
}

func (m *mockRegistryService) GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error) {
	return m.GetCurrentPriceByCoinGeckoID(ctx, coinGeckoID)
}

// mockWalletRepository implements manual.WalletRepository for testing
type mockWalletRepository struct {
	wallets map[uuid.UUID]*wallet.Wallet
}

func (m *mockWalletRepository) GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error) {
	if w, ok := m.wallets[walletID]; ok {
		return w, nil
	}
	return nil, nil
}

// Manual Income Handler Tests

func TestManualIncomeHandler_Integration_BasicIncome(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000", // 1 BTC in satoshi
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx)

	assert.Equal(t, ledger.TransactionStatusCompleted, tx.Status)
	assert.Len(t, tx.Entries, 2)

	// Check balance increased
	balance, err := svc.GetBalance(ctx, walletID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(100000000)))
}

func TestManualIncomeHandler_Integration_WithManualPrice(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	manualUSDRate := "6000000000000" // $60,000 * 10^8

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000",
			"usd_rate":       manualUSDRate,
		},
	)
	require.NoError(t, err)

	// Verify the manual rate was used
	for _, entry := range tx.Entries {
		expected := new(big.Int)
		expected.SetString(manualUSDRate, 10)
		assert.Equal(t, 0, entry.USDRate.Cmp(expected), "Entry should use manual USD rate")
	}
}

func TestManualIncomeHandler_Integration_MultipleAssets(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Add BTC
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Add ETH
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "ETH",
			"price_asset_id": "ethereum",
			"decimals":       18,
			"amount":         "1000000000000000000", // 1 ETH in wei
			"usd_rate":       "300000000000",
		},
	)
	require.NoError(t, err)

	// Check separate balances
	btcBalance, err := svc.GetBalance(ctx, walletID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, btcBalance.Cmp(big.NewInt(100000000)))

	ethBalance, err := svc.GetBalance(ctx, walletID, "ETH")
	require.NoError(t, err)
	expectedETH := new(big.Int)
	expectedETH.SetString("1000000000000000000", 10)
	assert.Equal(t, 0, ethBalance.Cmp(expectedETH))
}

func TestManualIncomeHandler_Integration_LargeAmount(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// 21 million BTC in satoshi (max supply)
	largeAmount := "2100000000000000"

	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         largeAmount,
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Verify no overflow
	balance, err := svc.GetBalance(ctx, walletID, "BTC")
	require.NoError(t, err)
	expected := new(big.Int)
	expected.SetString(largeAmount, 10)
	assert.Equal(t, 0, balance.Cmp(expected))
}

// Manual Outcome Handler Tests

func TestManualOutcomeHandler_Integration_BasicOutcome(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// First add income
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000", // 1 BTC
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Now withdraw half
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "50000000", // 0.5 BTC
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Check balance decreased
	balance, err := svc.GetBalance(ctx, walletID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(50000000)))
}

func TestManualOutcomeHandler_Integration_InsufficientBalance(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Add 1 BTC
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Try to withdraw 2 BTC (more than balance)
	tx, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "200000000", // 2 BTC - more than balance
			"usd_rate":       "5000000000000",
		},
	)

	// Should fail
	assert.Error(t, err)
	if tx != nil {
		assert.Equal(t, ledger.TransactionStatusFailed, tx.Status)
	}

	// Balance should remain unchanged
	balance, err := svc.GetBalance(ctx, walletID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(100000000)))
}

func TestManualOutcomeHandler_Integration_ExactBalance(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Add 1 BTC
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Withdraw exact balance
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000", // Exact balance
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Balance should be zero
	balance, err := svc.GetBalance(ctx, walletID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(0)))
}

func TestManualOutcomeHandler_Integration_SequentialOutcomes(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Add 1 BTC
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-4*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Withdraw 0.3 BTC
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-3*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "30000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Withdraw 0.2 BTC
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "20000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Balance should be 0.5 BTC
	balance, err := svc.GetBalance(ctx, walletID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(50000000)))
}

// E2E Flow Tests

func TestE2E_IncomeOutcomeSequence(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createTestWallet(t, ctx, testDB.Pool, userID)

	// Income: +10 BTC
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-4*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "1000000000", // 10 BTC
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Outcome: -3 BTC
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-3*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "300000000", // 3 BTC
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Outcome: -2 BTC
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletID.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "200000000", // 2 BTC
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Final balance should be 5 BTC
	balance, err := svc.GetBalance(ctx, walletID, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balance.Cmp(big.NewInt(500000000)))
}

func TestE2E_MultiWalletTransfers(t *testing.T) {
	ctx, svc := setupTest(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletA := createTestWallet(t, ctx, testDB.Pool, userID)
	walletB := createTestWallet(t, ctx, testDB.Pool, userID)

	// Add to Wallet A
	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-3*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletA.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "100000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Withdraw from Wallet A
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualOutcome,
		"manual",
		nil,
		time.Now().Add(-2*time.Hour),
		map[string]interface{}{
			"wallet_id":      walletA.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "50000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Add to Wallet B
	_, err = svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id":      walletB.String(),
			"asset_id":       "BTC",
			"price_asset_id": "bitcoin",
			"decimals":       8,
			"amount":         "200000000",
			"usd_rate":       "5000000000000",
		},
	)
	require.NoError(t, err)

	// Check independent balances
	balanceA, err := svc.GetBalance(ctx, walletA, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balanceA.Cmp(big.NewInt(50000000)))

	balanceB, err := svc.GetBalance(ctx, walletB, "BTC")
	require.NoError(t, err)
	assert.Equal(t, 0, balanceB.Cmp(big.NewInt(200000000)))
}
