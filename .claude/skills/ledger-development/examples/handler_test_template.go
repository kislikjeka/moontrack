// Package example provides test templates for ledger handler development.
// Copy and adapt this template when creating tests for new transaction handlers.
package example

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/internal/transport/httpapi/middleware"
)

// =============================================================================
// Mock Implementations
// =============================================================================

// mockPriceService provides mock price data for tests
type mockPriceService struct {
	currentPrice    *big.Int
	historicalPrice *big.Int
	err             error
}

func (m *mockPriceService) GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.currentPrice, nil
}

func (m *mockPriceService) GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.historicalPrice != nil {
		return m.historicalPrice, nil
	}
	return m.currentPrice, nil
}

// mockWalletRepository provides mock wallet data for tests
type mockWalletRepository struct {
	wallet *wallet.Wallet
	err    error
}

func (m *mockWalletRepository) GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.wallet, nil
}

// mockBalanceGetter provides mock balance data for tests
type mockBalanceGetter struct {
	balance *big.Int
	err     error
}

func (m *mockBalanceGetter) GetBalance(ctx context.Context, walletID uuid.UUID, assetID string) (*big.Int, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.balance, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// contextWithUserID creates a context with the given user ID
func contextWithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, middleware.UserIDKey, userID)
}

// mustParseBigInt parses a string to big.Int, panics on failure (use only in tests)
func mustParseBigInt(t *testing.T, s string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("failed to parse big.Int from %s", s)
	}
	return n
}

// =============================================================================
// Handler Entry Generation Tests
// =============================================================================

// TestYourHandler_Handle_GeneratesBalancedEntries verifies that the handler
// generates properly balanced ledger entries (SUM(debits) = SUM(credits))
func TestYourHandler_Handle_GeneratesBalancedEntries(t *testing.T) {
	walletID := uuid.New()
	ownerID := uuid.New()

	mockPrice := &mockPriceService{
		currentPrice: mustParseBigInt(t, "5000000000000"), // $50,000 * 10^8
	}

	mockWallet := &mockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: ownerID,
			Name:   "Test Wallet",
		},
	}

	// TODO: Replace with your handler constructor
	// handler := yourmodule.NewYourHandler(mockPrice, mockWallet)
	_ = mockPrice
	_ = mockWallet

	ctx := contextWithUserID(context.Background(), ownerID)

	tests := []struct {
		name            string
		data            map[string]interface{}
		expectedEntries int
		wantErr         bool
	}{
		{
			name: "basic income generates 2 balanced entries",
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "BTC",
				"decimals":    float64(8),
				"amount":      "100000000", // 1 BTC in satoshis
				"usd_rate":    "5000000000000",
				"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			},
			expectedEntries: 2,
			wantErr:         false,
		},
		{
			name: "large amount preserves precision",
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "ETH",
				"decimals":    float64(18),
				"amount":      "1000000000000000000000", // 1000 ETH in wei
				"usd_rate":    "200000000000",           // $2000 * 10^8
				"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			},
			expectedEntries: 2,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO: Uncomment and use your handler
			// entries, err := handler.Handle(ctx, tt.data)

			// Placeholder for template compilation
			var entries []*ledger.Entry
			var err error
			_ = ctx
			_ = tt.data

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, entries, tt.expectedEntries)

			// CRITICAL: Verify double-entry balance invariant
			debitSum := new(big.Int)
			creditSum := new(big.Int)

			for _, entry := range entries {
				require.NotNil(t, entry.Amount, "Entry amount must not be nil")
				require.GreaterOrEqual(t, entry.Amount.Sign(), 0, "Entry amount must be non-negative")

				if entry.DebitCredit == ledger.Debit {
					debitSum.Add(debitSum, entry.Amount)
				} else {
					creditSum.Add(creditSum, entry.Amount)
				}
			}

			assert.Equal(t, 0, debitSum.Cmp(creditSum),
				"Ledger entries must balance: SUM(debits)=%s must equal SUM(credits)=%s",
				debitSum.String(), creditSum.String())

			// Verify all entries have required fields
			for i, entry := range entries {
				assert.NotEmpty(t, entry.AssetID, "Entry %d must have AssetID", i)
				assert.NotNil(t, entry.USDRate, "Entry %d must have USDRate", i)
				assert.NotNil(t, entry.USDValue, "Entry %d must have USDValue", i)
				assert.NotZero(t, entry.OccurredAt, "Entry %d must have OccurredAt", i)
			}
		})
	}
}

// =============================================================================
// Authorization Tests
// =============================================================================

// TestYourHandler_ValidateData_ChecksWalletOwnership verifies that the handler
// properly checks wallet ownership before allowing operations
func TestYourHandler_ValidateData_ChecksWalletOwnership(t *testing.T) {
	walletID := uuid.New()
	ownerID := uuid.New()
	attackerID := uuid.New()

	mockPrice := &mockPriceService{
		currentPrice: mustParseBigInt(t, "5000000000000"),
	}

	mockWallet := &mockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: ownerID,
			Name:   "Owner's Wallet",
		},
	}

	// TODO: Replace with your handler constructor
	// handler := yourmodule.NewYourHandler(mockPrice, mockWallet)
	_ = mockPrice
	_ = mockWallet

	tests := []struct {
		name      string
		userID    uuid.UUID
		expectErr bool
	}{
		{
			name:      "owner can access their wallet",
			userID:    ownerID,
			expectErr: false,
		},
		{
			name:      "attacker cannot access another user's wallet",
			userID:    attackerID,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := contextWithUserID(context.Background(), tt.userID)

			data := map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "BTC",
				"decimals":    float64(8),
				"amount":      "100000000",
				"usd_rate":    "5000000000000",
				"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			}

			// TODO: Uncomment and use your handler
			// err := handler.ValidateData(ctx, data)
			var err error
			_ = ctx
			_ = data

			if tt.expectErr {
				assert.Error(t, err)
				// TODO: Check for specific error type
				// assert.ErrorIs(t, err, yourmodule.ErrUnauthorized)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Input Validation Tests
// =============================================================================

// TestYourHandler_ValidateData_InputValidation verifies that the handler
// properly validates all input fields
func TestYourHandler_ValidateData_InputValidation(t *testing.T) {
	walletID := uuid.New()
	ownerID := uuid.New()

	mockPrice := &mockPriceService{
		currentPrice: mustParseBigInt(t, "5000000000000"),
	}

	mockWallet := &mockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: ownerID,
			Name:   "Test Wallet",
		},
	}

	// TODO: Replace with your handler constructor
	// handler := yourmodule.NewYourHandler(mockPrice, mockWallet)
	_ = mockPrice
	_ = mockWallet

	ctx := contextWithUserID(context.Background(), ownerID)

	tests := []struct {
		name        string
		data        map[string]interface{}
		expectErr   bool
		errContains string
	}{
		{
			name: "valid data passes validation",
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "BTC",
				"decimals":    float64(8),
				"amount":      "100000000",
				"usd_rate":    "5000000000000",
				"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			},
			expectErr: false,
		},
		{
			name: "missing wallet_id fails",
			data: map[string]interface{}{
				"asset_id":    "BTC",
				"amount":      "100000000",
				"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			},
			expectErr:   true,
			errContains: "wallet",
		},
		{
			name: "invalid wallet_id format fails",
			data: map[string]interface{}{
				"wallet_id":   "not-a-uuid",
				"asset_id":    "BTC",
				"amount":      "100000000",
				"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			},
			expectErr:   true,
			errContains: "invalid",
		},
		{
			name: "missing asset_id fails",
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"amount":      "100000000",
				"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			},
			expectErr:   true,
			errContains: "asset",
		},
		{
			name: "negative amount fails",
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "BTC",
				"amount":      "-100000000",
				"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
			},
			expectErr:   true,
			errContains: "negative",
		},
		{
			name: "future date fails",
			data: map[string]interface{}{
				"wallet_id":   walletID.String(),
				"asset_id":    "BTC",
				"amount":      "100000000",
				"occurred_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339), // Future!
			},
			expectErr:   true,
			errContains: "future",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO: Uncomment and use your handler
			// err := handler.ValidateData(ctx, tt.data)
			var err error
			_ = ctx

			if tt.expectErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Price Source Tests
// =============================================================================

// TestYourHandler_Handle_RecordsPriceSource verifies that the handler
// properly records price source information in entry metadata
func TestYourHandler_Handle_RecordsPriceSource(t *testing.T) {
	walletID := uuid.New()
	ownerID := uuid.New()

	mockPrice := &mockPriceService{
		currentPrice: mustParseBigInt(t, "5000000000000"),
	}

	mockWallet := &mockWalletRepository{
		wallet: &wallet.Wallet{
			ID:     walletID,
			UserID: ownerID,
			Name:   "Test Wallet",
		},
	}

	// TODO: Replace with your handler constructor
	// handler := yourmodule.NewYourHandler(mockPrice, mockWallet)
	_ = mockPrice
	_ = mockWallet

	ctx := contextWithUserID(context.Background(), ownerID)

	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"asset_id":    "BTC",
		"decimals":    float64(8),
		"amount":      "100000000",
		"usd_rate":    "5000000000000",
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
	}

	// TODO: Uncomment and use your handler
	// entries, err := handler.Handle(ctx, data)
	var entries []*ledger.Entry
	var err error
	_ = ctx
	_ = data

	require.NoError(t, err)
	require.NotEmpty(t, entries)

	// Check that at least one entry has price source metadata
	hasPriceSource := false
	for _, entry := range entries {
		if entry.Metadata != nil {
			if _, ok := entry.Metadata["price_source"]; ok {
				hasPriceSource = true
				break
			}
		}
	}

	// Optional: Uncomment if price source tracking is required
	// assert.True(t, hasPriceSource, "Entries should include price source metadata")
	_ = hasPriceSource
}
