package integration

import (
	"context"
	"math/big"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPortfolio_ReflectsAllWalletBalances verifies that the portfolio service
// correctly aggregates balances from all user wallets (T137)
func TestPortfolio_ReflectsAllWalletBalances(t *testing.T) {
	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Initialize all required services
	ledgerRepo := setupLedgerRepository(db)
	ledgerService := setupLedgerService(ledgerRepo)
	walletService := setupWalletService(db)
	priceService := setupMockPriceService()
	portfolioService := setupPortfolioService(ledgerRepo, priceService)

	// Create test user
	userID := uuid.New()
	createTestUser(t, db, userID, "portfolio-test@example.com")

	// SCENARIO: User creates 3 wallets on different chains
	wallet1, err := walletService.Create(ctx, userID, "Ethereum Main", "ethereum", nil)
	require.NoError(t, err)

	wallet2, err := walletService.Create(ctx, userID, "Bitcoin Cold Storage", "bitcoin", nil)
	require.NoError(t, err)

	wallet3, err := walletService.Create(ctx, userID, "Solana Trading", "solana", nil)
	require.NoError(t, err)

	// Setup price mocks
	usdRates := map[string]*big.Int{
		"ETH": big.NewInt(300000000000),   // $3,000 * 10^8
		"BTC": big.NewInt(4500000000000),  // $45,000 * 10^8
		"SOL": big.NewInt(10000000000),    // $100 * 10^8
		"USDC": big.NewInt(100000000),     // $1 * 10^8
	}
	priceService.SetMockPrices(usdRates)

	// Add assets to wallets via manual income transactions
	incomeHandler := setupIncomeHandler(ledgerService, priceService)

	// Wallet 1: 10 ETH + 500 USDC
	_, err = incomeHandler.Execute(ctx, userID, &ManualIncomeTransaction{
		WalletID:    wallet1.ID,
		AssetID:     "ETH",
		Amount:      big.NewInt(10000000000000000000), // 10 ETH
		USDRate:     usdRates["ETH"],
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	})
	require.NoError(t, err)

	_, err = incomeHandler.Execute(ctx, userID, &ManualIncomeTransaction{
		WalletID:    wallet1.ID,
		AssetID:     "USDC",
		Amount:      big.NewInt(500000000), // 500 USDC
		USDRate:     usdRates["USDC"],
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	})
	require.NoError(t, err)

	// Wallet 2: 1.5 BTC
	_, err = incomeHandler.Execute(ctx, userID, &ManualIncomeTransaction{
		WalletID:    wallet2.ID,
		AssetID:     "BTC",
		Amount:      big.NewInt(150000000), // 1.5 BTC
		USDRate:     usdRates["BTC"],
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	})
	require.NoError(t, err)

	// Wallet 3: 50 SOL + 5 ETH (same asset as wallet 1)
	_, err = incomeHandler.Execute(ctx, userID, &ManualIncomeTransaction{
		WalletID:    wallet3.ID,
		AssetID:     "SOL",
		Amount:      big.NewInt(50000000000), // 50 SOL
		USDRate:     usdRates["SOL"],
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	})
	require.NoError(t, err)

	_, err = incomeHandler.Execute(ctx, userID, &ManualIncomeTransaction{
		WalletID:    wallet3.ID,
		AssetID:     "ETH",
		Amount:      big.NewInt(5000000000000000000), // 5 ETH
		USDRate:     usdRates["ETH"],
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	})
	require.NoError(t, err)

	// EXECUTE: Get portfolio
	portfolio, err := portfolioService.GetPortfolio(ctx, userID)

	// VERIFY: Portfolio correctly reflects all balances
	require.NoError(t, err)
	assert.NotNil(t, portfolio)

	// Verify total USD value
	// (15 ETH * $3,000) + (1.5 BTC * $45,000) + (50 SOL * $100) + (500 USDC * $1)
	// = $45,000 + $67,500 + $5,000 + $500 = $118,000
	expectedTotal := big.NewInt(11800000000000) // $118,000 * 10^8
	assert.Equal(t, expectedTotal.String(), portfolio.TotalUSDValue.String(),
		"Total portfolio value should be $118,000")

	// Verify asset aggregation
	assert.Len(t, portfolio.Assets, 4, "Should have 4 different asset types")

	// ETH should be aggregated: 10 (wallet1) + 5 (wallet3) = 15 ETH total
	ethAsset := findPortfolioAsset(portfolio.Assets, "ETH")
	require.NotNil(t, ethAsset, "ETH should be in portfolio")
	expectedETH := big.NewInt(15000000000000000000) // 15 ETH
	assert.Equal(t, expectedETH.String(), ethAsset.Balance.String(),
		"ETH balance should be aggregated from wallet1 and wallet3")
	assert.Len(t, ethAsset.Wallets, 2, "ETH should be in 2 different wallets")

	// BTC from wallet 2
	btcAsset := findPortfolioAsset(portfolio.Assets, "BTC")
	require.NotNil(t, btcAsset, "BTC should be in portfolio")
	expectedBTC := big.NewInt(150000000) // 1.5 BTC
	assert.Equal(t, expectedBTC.String(), btcAsset.Balance.String())
	assert.Len(t, btcAsset.Wallets, 1, "BTC should be in 1 wallet")

	// SOL from wallet 3
	solAsset := findPortfolioAsset(portfolio.Assets, "SOL")
	require.NotNil(t, solAsset, "SOL should be in portfolio")
	expectedSOL := big.NewInt(50000000000) // 50 SOL
	assert.Equal(t, expectedSOL.String(), solAsset.Balance.String())

	// USDC from wallet 1
	usdcAsset := findPortfolioAsset(portfolio.Assets, "USDC")
	require.NotNil(t, usdcAsset, "USDC should be in portfolio")
	expectedUSDC := big.NewInt(500000000) // 500 USDC
	assert.Equal(t, expectedUSDC.String(), usdcAsset.Balance.String())

	// Verify individual wallet balances are tracked
	for _, asset := range portfolio.Assets {
		for _, walletAsset := range asset.Wallets {
			balance, err := ledgerRepo.GetBalance(ctx, walletAsset.WalletID, asset.AssetID)
			require.NoError(t, err)

			assert.Equal(t, walletAsset.Balance.String(), balance.String(),
				"Wallet balance should match ledger for %s in wallet %s",
				asset.AssetID, walletAsset.WalletID)
		}
	}
}

// TestPortfolio_UpdatesAfterTransactions verifies that portfolio reflects
// balance changes after transactions
func TestPortfolio_UpdatesAfterTransactions(t *testing.T) {
	ctx := context.Background()
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ledgerRepo := setupLedgerRepository(db)
	ledgerService := setupLedgerService(ledgerRepo)
	walletService := setupWalletService(db)
	priceService := setupMockPriceService()
	portfolioService := setupPortfolioService(ledgerRepo, priceService)

	userID := uuid.New()
	createTestUser(t, db, userID, "update-test@example.com")

	wallet, err := walletService.Create(ctx, userID, "Test Wallet", "ethereum", nil)
	require.NoError(t, err)

	usdRate := big.NewInt(300000000000) // $3,000
	priceService.SetMockPrices(map[string]*big.Int{"ETH": usdRate})

	// Initial state: Empty portfolio
	portfolio1, err := portfolioService.GetPortfolio(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, "0", portfolio1.TotalUSDValue.String(), "Initial portfolio should be empty")

	// Add 10 ETH
	incomeHandler := setupIncomeHandler(ledgerService, priceService)
	_, err = incomeHandler.Execute(ctx, userID, &ManualIncomeTransaction{
		WalletID:    wallet.ID,
		AssetID:     "ETH",
		Amount:      big.NewInt(10000000000000000000), // 10 ETH
		USDRate:     usdRate,
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	})
	require.NoError(t, err)

	// Portfolio should reflect addition
	portfolio2, err := portfolioService.GetPortfolio(ctx, userID)
	require.NoError(t, err)
	expectedValue := big.NewInt(3000000000000) // 10 ETH * $3,000 = $30,000
	assert.Equal(t, expectedValue.String(), portfolio2.TotalUSDValue.String(),
		"Portfolio should reflect added ETH")

	// Withdraw 3 ETH
	outcomeHandler := setupOutcomeHandler(ledgerService, ledgerRepo, priceService)
	_, err = outcomeHandler.Execute(ctx, userID, &ManualOutcomeTransaction{
		WalletID:    wallet.ID,
		AssetID:     "ETH",
		Amount:      big.NewInt(3000000000000000000), // 3 ETH
		USDRate:     usdRate,
		OccurredAt:  time.Now(),
		PriceSource: "manual",
	})
	require.NoError(t, err)

	// Portfolio should reflect withdrawal
	portfolio3, err := portfolioService.GetPortfolio(ctx, userID)
	require.NoError(t, err)
	expectedValue = big.NewInt(2100000000000) // 7 ETH * $3,000 = $21,000
	assert.Equal(t, expectedValue.String(), portfolio3.TotalUSDValue.String(),
		"Portfolio should reflect withdrawn ETH")

	// Verify final balance
	ethAsset := findPortfolioAsset(portfolio3.Assets, "ETH")
	require.NotNil(t, ethAsset)
	expectedBalance := big.NewInt(7000000000000000000) // 7 ETH
	assert.Equal(t, expectedBalance.String(), ethAsset.Balance.String(),
		"Final balance should be 10 - 3 = 7 ETH")
}

// Helper functions

func setupPortfolioService(ledgerRepo LedgerRepository, priceService PriceService) *PortfolioService {
	// TODO: Initialize portfolio service
	return nil
}

func findPortfolioAsset(assets []PortfolioAsset, assetID string) *PortfolioAsset {
	for i := range assets {
		if assets[i].AssetID == assetID {
			return &assets[i]
		}
	}
	return nil
}

// Type stubs

type PortfolioService struct{}
type PortfolioAsset struct {
	AssetID  string
	Balance  *big.Int
	USDValue *big.Int
	Wallets  []WalletAsset
}

type WalletAsset struct {
	WalletID   uuid.UUID
	WalletName string
	Balance    *big.Int
}

type Portfolio struct {
	UserID        uuid.UUID
	TotalUSDValue *big.Int
	Assets        []PortfolioAsset
	UpdatedAt     time.Time
}
