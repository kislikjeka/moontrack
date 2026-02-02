package contract

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGETPortfolio_ReturnsAccurateTotals verifies the GET /portfolio endpoint
// returns accurate total portfolio value and asset breakdown (T136)
func TestGETPortfolio_ReturnsAccurateTotals(t *testing.T) {
	// Setup test database and services
	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create test user
	userID := uuid.New()
	authToken := createTestUserWithAuth(t, db, userID, "portfolio@example.com")

	// Setup test scenario: User has multiple wallets with assets
	wallet1 := createTestWallet(t, db, userID, "Main Wallet", "ethereum")
	wallet2 := createTestWallet(t, db, userID, "Trading Wallet", "ethereum")

	// Add balances via transactions
	addTestBalance(t, db, userID, wallet1, "BTC", big.NewInt(200000000))  // 2 BTC
	addTestBalance(t, db, userID, wallet1, "ETH", big.NewInt(5000000000000000000))  // 5 ETH
	addTestBalance(t, db, userID, wallet2, "ETH", big.NewInt(3000000000000000000))  // 3 ETH (same asset, different wallet)
	addTestBalance(t, db, userID, wallet2, "USDC", big.NewInt(1000000000))  // 1000 USDC

	// Mock price service (prices in production would come from CoinGecko)
	mockPriceService := setupMockPriceServiceWithPrices(map[string]*big.Int{
		"BTC":  big.NewInt(4500000000000),  // $45,000
		"ETH":  big.NewInt(300000000000),   // $3,000
		"USDC": big.NewInt(100000000),      // $1
	})

	// Create HTTP server with portfolio handler
	handler := setupPortfolioHandler(db, mockPriceService)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Make GET /portfolio request
	req, err := http.NewRequest("GET", server.URL+"/portfolio", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Verify response
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var portfolio PortfolioResponse
	err = json.NewDecoder(resp.Body).Decode(&portfolio)
	require.NoError(t, err)

	// Verify total value: (2 BTC * $45k) + (8 ETH * $3k) + (1000 USDC * $1)
	//                   = $90,000 + $24,000 + $1,000 = $115,000
	expectedTotal := "$115,000.00"
	assert.Equal(t, expectedTotal, portfolio.TotalValue, "Total portfolio value should be $115,000")

	// Verify asset breakdown
	assert.Len(t, portfolio.Assets, 3, "Should have 3 different asset types")

	// Check BTC
	btcAsset := findAssetInResponse(portfolio.Assets, "BTC")
	require.NotNil(t, btcAsset, "BTC should be in portfolio response")
	assert.Equal(t, "2.00000000", btcAsset.Balance)
	assert.Equal(t, "$90,000.00", btcAsset.USDValue)

	// Check ETH (aggregated from both wallets: 5 + 3 = 8 ETH)
	ethAsset := findAssetInResponse(portfolio.Assets, "ETH")
	require.NotNil(t, ethAsset, "ETH should be in portfolio response")
	assert.Equal(t, "8.000000000000000000", ethAsset.Balance)
	assert.Equal(t, "$24,000.00", ethAsset.USDValue)

	// Check USDC
	usdcAsset := findAssetInResponse(portfolio.Assets, "USDC")
	require.NotNil(t, usdcAsset, "USDC should be in portfolio response")
	assert.Equal(t, "1000.000000", usdcAsset.Balance)
	assert.Equal(t, "$1,000.00", usdcAsset.USDValue)

	// Verify price timestamps are included
	assert.NotEmpty(t, portfolio.PriceUpdatedAt, "Should include price update timestamp")
}

// TestGETPortfolio_RequiresAuthentication verifies the endpoint is protected
func TestGETPortfolio_RequiresAuthentication(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	handler := setupPortfolioHandler(db, setupMockPriceService())
	server := httptest.NewServer(handler)
	defer server.Close()

	// Request without auth token
	resp, err := http.Get(server.URL + "/portfolio")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "Should reject unauthenticated requests")
}

// TestGETPortfolio_HandlesEmptyPortfolio verifies behavior for new users
func TestGETPortfolio_HandlesEmptyPortfolio(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create user with no wallets/assets
	userID := uuid.New()
	authToken := createTestUserWithAuth(t, db, userID, "empty@example.com")

	handler := setupPortfolioHandler(db, setupMockPriceService())
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL+"/portfolio", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var portfolio PortfolioResponse
	err = json.NewDecoder(resp.Body).Decode(&portfolio)
	require.NoError(t, err)

	assert.Equal(t, "$0.00", portfolio.TotalValue, "Empty portfolio should have $0 value")
	assert.Len(t, portfolio.Assets, 0, "Empty portfolio should have no assets")
}

// TestGETPortfolio_HandlesPriceAPIFailure verifies graceful degradation
func TestGETPortfolio_HandlesPriceAPIFailure(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	userID := uuid.New()
	authToken := createTestUserWithAuth(t, db, userID, "pricetest@example.com")

	wallet := createTestWallet(t, db, userID, "Test Wallet", "ethereum")
	addTestBalance(t, db, userID, wallet, "BTC", big.NewInt(100000000)) // 1 BTC

	// Price service that returns errors
	mockPriceService := setupFailingPriceService()

	handler := setupPortfolioHandler(db, mockPriceService)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL+"/portfolio", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should still return 200 with partial data (or stale prices)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Should handle price failures gracefully")

	var portfolio PortfolioResponse
	err = json.NewDecoder(resp.Body).Decode(&portfolio)
	require.NoError(t, err)

	// Should have the asset even if price is unavailable
	assert.Len(t, portfolio.Assets, 1, "Should still show assets")
	btcAsset := findAssetInResponse(portfolio.Assets, "BTC")
	require.NotNil(t, btcAsset)
	assert.Equal(t, "1.00000000", btcAsset.Balance, "Balance should be shown even without price")
}

// Helper types

type PortfolioResponse struct {
	TotalValue      string          `json:"total_value"`
	Assets          []AssetResponse `json:"assets"`
	PriceUpdatedAt  string          `json:"price_updated_at"`
}

type AssetResponse struct {
	AssetID  string `json:"asset_id"`
	Balance  string `json:"balance"`
	USDValue string `json:"usd_value"`
	USDPrice string `json:"usd_price,omitempty"`
}

func findAssetInResponse(assets []AssetResponse, assetID string) *AssetResponse {
	for i := range assets {
		if assets[i].AssetID == assetID {
			return &assets[i]
		}
	}
	return nil
}

// Helper functions (implementations depend on actual test infrastructure)

func setupPortfolioHandler(db *sql.DB, priceService PriceService) http.Handler {
	// TODO: Create chi router with portfolio handler
	return nil
}

func setupMockPriceServiceWithPrices(prices map[string]*big.Int) PriceService {
	// TODO: Return mock price service with predefined prices
	return nil
}

func setupFailingPriceService() PriceService {
	// TODO: Return mock price service that returns errors
	return nil
}

func createTestWallet(t *testing.T, db *sql.DB, userID uuid.UUID, name, chain string) uuid.UUID {
	// TODO: Insert test wallet and return ID
	return uuid.New()
}

func addTestBalance(t *testing.T, db *sql.DB, userID, walletID uuid.UUID, assetID string, amount *big.Int) {
	// TODO: Create income transaction to add balance
}
