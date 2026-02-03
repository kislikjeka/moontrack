package manual_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/module/manual"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Authorization Tests for Manual Transaction Handlers
// These tests verify that users can only create transactions on their own wallets

// Helper to create a context with user ID
func contextWithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, middleware.UserIDKey, userID)
}

// authMockRegistryService is a mock for RegistryService (auth tests)
type authMockRegistryService struct {
	currentPrice *big.Int
	err          error
}

func (m *authMockRegistryService) GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.currentPrice, nil
}

func (m *authMockRegistryService) GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.currentPrice, nil
}

// authMockWalletRepository is a mock for WalletRepository (auth tests)
type authMockWalletRepository struct {
	wallet *wallet.Wallet
	err    error
}

func (m *authMockWalletRepository) GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.wallet, nil
}

// authMockBalanceGetter is a mock for BalanceGetter (auth tests)
type authMockBalanceGetter struct {
	balance *big.Int
	err     error
}

func (m *authMockBalanceGetter) GetBalance(ctx context.Context, walletID uuid.UUID, assetID string) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.balance, nil
}

// TestManualIncomeHandler_ValidateData_ChecksOwnership tests that ValidateData checks wallet ownership
func TestManualIncomeHandler_ValidateData_ChecksOwnership(t *testing.T) {
	walletID := uuid.New()
	walletOwnerID := uuid.New()

	mockRegistry := &authMockRegistryService{
		currentPrice: big.NewInt(5000000000000),
	}

	mockWalletRepo := &authMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: walletOwnerID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// Create context with different user ID (not the owner)
	differentUserID := uuid.New()
	ctx := contextWithUserID(context.Background(), differentUserID)

	rate, _ := money.NewBigIntFromString("5000000000000")
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "100",
		"usd_rate":    rate.String(),
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"notes":       "Test deposit",
	}

	err := handler.ValidateData(ctx, data)
	assert.Error(t, err)
	assert.ErrorIs(t, err, manual.ErrUnauthorized)
}

// TestManualIncomeHandler_CrossUserWallet_ReturnsUnauthorized tests that user A cannot deposit to user B's wallet
func TestManualIncomeHandler_CrossUserWallet_ReturnsUnauthorized(t *testing.T) {
	// Setup: User A owns wallet W1, User B tries to deposit to W1
	walletID := uuid.New()
	userA := uuid.New() // Wallet owner
	userB := uuid.New() // Different user trying to deposit

	mockRegistry := &authMockRegistryService{
		currentPrice: big.NewInt(5000000000000),
	}

	mockWalletRepo := &authMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userA, // Wallet belongs to User A
			Name:   "User A's Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// User B tries to deposit to User A's wallet
	ctx := contextWithUserID(context.Background(), userB)

	rate, _ := money.NewBigIntFromString("5000000000000")
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "100",
		"usd_rate":    rate.String(),
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"notes":       "Unauthorized deposit attempt",
	}

	err := handler.ValidateData(ctx, data)
	require.Error(t, err)
	assert.ErrorIs(t, err, manual.ErrUnauthorized)
}

// TestManualIncomeHandler_OwnWallet_Succeeds tests that owner can deposit to their own wallet
func TestManualIncomeHandler_OwnWallet_Succeeds(t *testing.T) {
	walletID := uuid.New()
	ownerID := uuid.New()

	mockRegistry := &authMockRegistryService{
		currentPrice: big.NewInt(5000000000000),
	}

	mockWalletRepo := &authMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: ownerID,
			Name:   "Owner's Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// Owner tries to deposit to their own wallet
	ctx := contextWithUserID(context.Background(), ownerID)

	rate, _ := money.NewBigIntFromString("5000000000000")
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "100",
		"usd_rate":    rate.String(),
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"notes":       "Owner deposit",
	}

	err := handler.ValidateData(ctx, data)
	assert.NoError(t, err)
}

// TestManualOutcomeHandler_CrossUserWallet_ReturnsUnauthorized tests that user A cannot withdraw from user B's wallet
func TestManualOutcomeHandler_CrossUserWallet_ReturnsUnauthorized(t *testing.T) {
	walletID := uuid.New()
	userA := uuid.New() // Wallet owner
	userB := uuid.New() // Different user trying to withdraw

	mockRegistry := &authMockRegistryService{
		currentPrice: big.NewInt(5000000000000),
	}

	mockWalletRepo := &authMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: userA,
			Name:   "User A's Wallet",
		},
	}

	mockBalanceGetter := &authMockBalanceGetter{
		balance: big.NewInt(1000), // Has sufficient balance
	}

	handler := manual.NewManualOutcomeHandler(mockRegistry, mockWalletRepo, mockBalanceGetter)

	// User B tries to withdraw from User A's wallet
	ctx := contextWithUserID(context.Background(), userB)

	rate, _ := money.NewBigIntFromString("5000000000000")
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "100",
		"usd_rate":    rate.String(),
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"notes":       "Unauthorized withdrawal attempt",
	}

	err := handler.ValidateData(ctx, data)
	require.Error(t, err)
	assert.ErrorIs(t, err, manual.ErrUnauthorized)
}

// TestManualOutcomeHandler_OwnWallet_Succeeds tests that owner can withdraw from their own wallet
func TestManualOutcomeHandler_OwnWallet_Succeeds(t *testing.T) {
	walletID := uuid.New()
	ownerID := uuid.New()

	mockRegistry := &authMockRegistryService{
		currentPrice: big.NewInt(5000000000000),
	}

	mockWalletRepo := &authMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: ownerID,
			Name:   "Owner's Wallet",
		},
	}

	mockBalanceGetter := &authMockBalanceGetter{
		balance: big.NewInt(1000), // Has sufficient balance
	}

	handler := manual.NewManualOutcomeHandler(mockRegistry, mockWalletRepo, mockBalanceGetter)

	// Owner tries to withdraw from their own wallet
	ctx := contextWithUserID(context.Background(), ownerID)

	rate, _ := money.NewBigIntFromString("5000000000000")
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "100",
		"usd_rate":    rate.String(),
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"notes":       "Owner withdrawal",
	}

	err := handler.ValidateData(ctx, data)
	assert.NoError(t, err)
}

// TestManualIncomeHandler_NoUserInContext_AllowsOperation tests that operations without
// user context are allowed (for internal/system operations)
func TestManualIncomeHandler_NoUserInContext_AllowsOperation(t *testing.T) {
	walletID := uuid.New()
	ownerID := uuid.New()

	mockRegistry := &authMockRegistryService{
		currentPrice: big.NewInt(5000000000000),
	}

	mockWalletRepo := &authMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: ownerID,
			Name:   "Test Wallet",
		},
	}

	handler := manual.NewManualIncomeHandler(mockRegistry, mockWalletRepo)

	// Context WITHOUT user ID (e.g., system/internal operation)
	ctx := context.Background()

	rate, _ := money.NewBigIntFromString("5000000000000")
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "100",
		"usd_rate":    rate.String(),
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"notes":       "System deposit",
	}

	// Should succeed when no user in context (system/internal operation)
	err := handler.ValidateData(ctx, data)
	assert.NoError(t, err)
}

// TestManualOutcomeHandler_NoUserInContext_AllowsOperation tests that operations without
// user context are allowed (for internal/system operations)
func TestManualOutcomeHandler_NoUserInContext_AllowsOperation(t *testing.T) {
	walletID := uuid.New()
	ownerID := uuid.New()

	mockRegistry := &authMockRegistryService{
		currentPrice: big.NewInt(5000000000000),
	}

	mockWalletRepo := &authMockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: ownerID,
			Name:   "Test Wallet",
		},
	}

	mockBalanceGetter := &authMockBalanceGetter{
		balance: big.NewInt(1000),
	}

	handler := manual.NewManualOutcomeHandler(mockRegistry, mockWalletRepo, mockBalanceGetter)

	// Context WITHOUT user ID (e.g., system/internal operation)
	ctx := context.Background()

	rate, _ := money.NewBigIntFromString("5000000000000")
	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "100",
		"usd_rate":    rate.String(),
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"notes":       "System withdrawal",
	}

	// Should succeed when no user in context (system/internal operation)
	err := handler.ValidateData(ctx, data)
	assert.NoError(t, err)
}
