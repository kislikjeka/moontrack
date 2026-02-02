package service

import (
	"context"
	"math/big"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPortfolioService_CalculatesTotalBalanceCorrectly verifies that portfolio
// service correctly calculates total balance from all wallet balances (T134)
func TestPortfolioService_CalculatesTotalBalanceCorrectly(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	ledgerRepo := setupMockLedgerRepository()
	priceService := setupMockPriceService()
	portfolioService := NewPortfolioService(ledgerRepo, priceService)

	userID := uuid.New()

	// Mock scenario: User has 3 wallets with different assets
	mockBalances := []AccountBalance{
		// Wallet 1: 2 BTC
		{WalletID: uuid.New(), AssetID: "BTC", Balance: big.NewInt(200000000)}, // 2.0 BTC (8 decimals)
		// Wallet 2: 10 ETH
		{WalletID: uuid.New(), AssetID: "ETH", Balance: big.NewInt(10000000000000000000)}, // 10 ETH (18 decimals)
		// Wallet 3: 1000 USDC
		{WalletID: uuid.New(), AssetID: "USDC", Balance: big.NewInt(1000000000)}, // 1000 USDC (6 decimals)
	}

	// Mock prices (scaled by 10^8)
	mockPrices := map[string]*big.Int{
		"BTC":  big.NewInt(4500000000000),  // $45,000 * 10^8
		"ETH":  big.NewInt(300000000000),   // $3,000 * 10^8
		"USDC": big.NewInt(100000000),      // $1 * 10^8
	}

	ledgerRepo.SetMockBalances(userID, mockBalances)
	priceService.SetMockPrices(mockPrices)

	// Execute
	portfolio, err := portfolioService.GetPortfolio(ctx, userID)

	// Verify
	require.NoError(t, err)
	assert.NotNil(t, portfolio)

	// Expected total: (2 BTC * $45,000) + (10 ETH * $3,000) + (1000 USDC * $1)
	//                = $90,000 + $30,000 + $1,000 = $121,000
	expectedTotal := big.NewInt(12100000000000) // $121,000 * 10^8
	assert.Equal(t, expectedTotal.String(), portfolio.TotalUSDValue.String(),
		"Total portfolio value should be $121,000")

	// Verify asset breakdown
	assert.Len(t, portfolio.Assets, 3, "Should have 3 different assets")

	// Check each asset
	btcAsset := findAssetByID(portfolio.Assets, "BTC")
	require.NotNil(t, btcAsset, "BTC should be in portfolio")
	assert.Equal(t, "200000000", btcAsset.Balance.String(), "BTC balance should be 2.0")
	assert.Equal(t, "9000000000000", btcAsset.USDValue.String(), "BTC value should be $90,000")

	ethAsset := findAssetByID(portfolio.Assets, "ETH")
	require.NotNil(t, ethAsset, "ETH should be in portfolio")
	assert.Equal(t, "10000000000000000000", ethAsset.Balance.String(), "ETH balance should be 10.0")
	assert.Equal(t, "3000000000000", ethAsset.USDValue.String(), "ETH value should be $30,000")

	usdcAsset := findAssetByID(portfolio.Assets, "USDC")
	require.NotNil(t, usdcAsset, "USDC should be in portfolio")
	assert.Equal(t, "1000000000", usdcAsset.Balance.String(), "USDC balance should be 1000")
	assert.Equal(t, "100000000000", usdcAsset.USDValue.String(), "USDC value should be $1,000")
}

// TestPortfolioService_AggregatesAssetsAcrossWallets verifies that assets
// with the same ID are aggregated across multiple wallets (T135)
func TestPortfolioService_AggregatesAssetsAcrossWallets(t *testing.T) {
	ctx := context.Background()

	ledgerRepo := setupMockLedgerRepository()
	priceService := setupMockPriceService()
	portfolioService := NewPortfolioService(ledgerRepo, priceService)

	userID := uuid.New()

	// Mock scenario: User has ETH in 3 different wallets
	wallet1 := uuid.New()
	wallet2 := uuid.New()
	wallet3 := uuid.New()

	mockBalances := []AccountBalance{
		{WalletID: wallet1, AssetID: "ETH", Balance: big.NewInt(5000000000000000000)},  // 5 ETH
		{WalletID: wallet2, AssetID: "ETH", Balance: big.NewInt(3000000000000000000)},  // 3 ETH
		{WalletID: wallet3, AssetID: "ETH", Balance: big.NewInt(2000000000000000000)},  // 2 ETH
		{WalletID: wallet1, AssetID: "BTC", Balance: big.NewInt(100000000)},            // 1 BTC
	}

	mockPrices := map[string]*big.Int{
		"ETH": big.NewInt(300000000000),  // $3,000 * 10^8
		"BTC": big.NewInt(4500000000000), // $45,000 * 10^8
	}

	ledgerRepo.SetMockBalances(userID, mockBalances)
	priceService.SetMockPrices(mockPrices)

	// Execute
	portfolio, err := portfolioService.GetPortfolio(ctx, userID)

	// Verify
	require.NoError(t, err)
	assert.Len(t, portfolio.Assets, 2, "Should have 2 different asset types")

	// Check ETH aggregation: 5 + 3 + 2 = 10 ETH total
	ethAsset := findAssetByID(portfolio.Assets, "ETH")
	require.NotNil(t, ethAsset, "ETH should be in portfolio")

	expectedETHBalance := big.NewInt(10000000000000000000) // 10 ETH
	assert.Equal(t, expectedETHBalance.String(), ethAsset.Balance.String(),
		"ETH should be aggregated from all wallets: 5 + 3 + 2 = 10 ETH")

	expectedETHValue := big.NewInt(3000000000000) // 10 ETH * $3,000 = $30,000
	assert.Equal(t, expectedETHValue.String(), ethAsset.USDValue.String(),
		"ETH total value should be $30,000")

	// Verify wallet breakdown shows individual wallets
	assert.Len(t, ethAsset.Wallets, 3, "ETH should be in 3 different wallets")
}

// TestPortfolioService_HandlesEmptyPortfolio verifies behavior when user has no assets (T136 coverage)
func TestPortfolioService_HandlesEmptyPortfolio(t *testing.T) {
	ctx := context.Background()

	ledgerRepo := setupMockLedgerRepository()
	priceService := setupMockPriceService()
	portfolioService := NewPortfolioService(ledgerRepo, priceService)

	userID := uuid.New()

	// No balances for this user
	ledgerRepo.SetMockBalances(userID, []AccountBalance{})

	// Execute
	portfolio, err := portfolioService.GetPortfolio(ctx, userID)

	// Verify
	require.NoError(t, err)
	assert.NotNil(t, portfolio)
	assert.Equal(t, "0", portfolio.TotalUSDValue.String(), "Empty portfolio should have $0 value")
	assert.Len(t, portfolio.Assets, 0, "Empty portfolio should have no assets")
}

// TestPortfolioService_HandlesPriceAPIFailure verifies graceful handling when prices unavailable (T136 coverage)
func TestPortfolioService_HandlesPriceAPIFailure(t *testing.T) {
	ctx := context.Background()

	ledgerRepo := setupMockLedgerRepository()
	priceService := setupMockPriceService()
	portfolioService := NewPortfolioService(ledgerRepo, priceService)

	userID := uuid.New()

	mockBalances := []AccountBalance{
		{WalletID: uuid.New(), AssetID: "BTC", Balance: big.NewInt(100000000)}, // 1 BTC
	}

	ledgerRepo.SetMockBalances(userID, mockBalances)
	// Price service returns error for BTC
	priceService.SetPriceError("BTC", ErrPriceUnavailable)

	// Execute
	portfolio, err := portfolioService.GetPortfolio(ctx, userID)

	// Verify
	require.NoError(t, err, "Portfolio service should handle price failures gracefully")
	assert.NotNil(t, portfolio)

	btcAsset := findAssetByID(portfolio.Assets, "BTC")
	require.NotNil(t, btcAsset)

	// Should have balance but USD value might be 0 or stale
	assert.Equal(t, "100000000", btcAsset.Balance.String())
	// USD value behavior depends on implementation: could be 0, cached, or marked unavailable
}

// Helper functions and types

type AccountBalance struct {
	WalletID uuid.UUID
	AssetID  string
	Balance  *big.Int
}

func setupMockLedgerRepository() *MockLedgerRepository {
	return &MockLedgerRepository{
		balances: make(map[uuid.UUID][]AccountBalance),
	}
}

func setupMockPriceService() *MockPriceService {
	return &MockPriceService{
		prices: make(map[string]*big.Int),
		errors: make(map[string]error),
	}
}

func findAssetByID(assets []PortfolioAsset, assetID string) *PortfolioAsset {
	for i := range assets {
		if assets[i].AssetID == assetID {
			return &assets[i]
		}
	}
	return nil
}

// Mock implementations

type MockLedgerRepository struct {
	balances map[uuid.UUID][]AccountBalance
}

func (m *MockLedgerRepository) SetMockBalances(userID uuid.UUID, balances []AccountBalance) {
	m.balances[userID] = balances
}

func (m *MockLedgerRepository) GetUserBalances(ctx context.Context, userID uuid.UUID) ([]AccountBalance, error) {
	return m.balances[userID], nil
}

type MockPriceService struct {
	prices map[string]*big.Int
	errors map[string]error
}

func (m *MockPriceService) SetMockPrices(prices map[string]*big.Int) {
	m.prices = prices
}

func (m *MockPriceService) SetPriceError(assetID string, err error) {
	m.errors[assetID] = err
}

func (m *MockPriceService) GetPrice(ctx context.Context, assetID string) (*big.Int, error) {
	if err, ok := m.errors[assetID]; ok {
		return nil, err
	}
	if price, ok := m.prices[assetID]; ok {
		return price, nil
	}
	return nil, ErrPriceNotFound
}

// Type stubs (replace with actual types from portfolio service)

type PortfolioAsset struct {
	AssetID   string
	Balance   *big.Int
	USDValue  *big.Int
	Wallets   []WalletAsset
}

type WalletAsset struct {
	WalletID  uuid.UUID
	WalletName string
	Balance   *big.Int
}

type Portfolio struct {
	UserID        uuid.UUID
	TotalUSDValue *big.Int
	Assets        []PortfolioAsset
	UpdatedAt     time.Time
}

var (
	ErrPriceUnavailable = errors.New("price unavailable")
	ErrPriceNotFound    = errors.New("price not found")
)
