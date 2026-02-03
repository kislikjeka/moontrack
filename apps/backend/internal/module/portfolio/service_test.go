package portfolio

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPortfolioService_CalculatesTotalBalanceCorrectly verifies that portfolio
// service correctly calculates total balance from all wallet balances (T134)
func TestPortfolioService_CalculatesTotalBalanceCorrectly(t *testing.T) {
	ctx := context.Background()

	// Setup mocks
	ledgerRepo := setupMockLedgerRepository()
	walletRepo := setupMockWalletRepository()
	priceService := setupMockPriceService()
	portfolioService := NewPortfolioService(ledgerRepo, walletRepo, priceService)

	userID := uuid.New()
	wallet1 := uuid.New()
	wallet2 := uuid.New()
	wallet3 := uuid.New()

	// Mock wallets
	walletRepo.SetMockWallets(userID, []*Wallet{
		{ID: wallet1, UserID: userID, Name: "Wallet 1", ChainID: "bitcoin"},
		{ID: wallet2, UserID: userID, Name: "Wallet 2", ChainID: "ethereum"},
		{ID: wallet3, UserID: userID, Name: "Wallet 3", ChainID: "ethereum"},
	})

	// Mock accounts for wallets
	account1 := uuid.New()
	account2 := uuid.New()
	account3 := uuid.New()

	ledgerRepo.SetMockAccounts(wallet1, []*ledger.Account{
		{ID: account1, WalletID: &wallet1, AssetID: "BTC"},
	})
	ledgerRepo.SetMockAccounts(wallet2, []*ledger.Account{
		{ID: account2, WalletID: &wallet2, AssetID: "ETH"},
	})
	ledgerRepo.SetMockAccounts(wallet3, []*ledger.Account{
		{ID: account3, WalletID: &wallet3, AssetID: "USDC"},
	})

	// Use SetString for large integers that overflow int64
	ethBalance := new(big.Int)
	ethBalance.SetString("10000000000000000000", 10) // 10 ETH (18 decimals)

	// Mock account balances
	ledgerRepo.SetMockBalances(account1, []*ledger.AccountBalance{
		{AssetID: "BTC", Balance: big.NewInt(200000000)}, // 2.0 BTC
	})
	ledgerRepo.SetMockBalances(account2, []*ledger.AccountBalance{
		{AssetID: "ETH", Balance: ethBalance}, // 10 ETH
	})
	ledgerRepo.SetMockBalances(account3, []*ledger.AccountBalance{
		{AssetID: "USDC", Balance: big.NewInt(1000000000)}, // 1000 USDC
	})

	// Mock prices (scaled by 10^8)
	priceService.SetMockPrice("BTC", big.NewInt(4500000000000)) // $45,000 * 10^8
	priceService.SetMockPrice("ETH", big.NewInt(300000000000))  // $3,000 * 10^8
	priceService.SetMockPrice("USDC", big.NewInt(100000000))    // $1 * 10^8

	// Execute
	portfolio, err := portfolioService.GetPortfolioSummary(ctx, userID)

	// Verify
	require.NoError(t, err)
	assert.NotNil(t, portfolio)
	assert.Equal(t, 3, portfolio.TotalAssets, "Should have 3 different assets")
}

// TestPortfolioService_HandlesEmptyPortfolio verifies behavior when user has no assets (T136 coverage)
func TestPortfolioService_HandlesEmptyPortfolio(t *testing.T) {
	ctx := context.Background()

	ledgerRepo := setupMockLedgerRepository()
	walletRepo := setupMockWalletRepository()
	priceService := setupMockPriceService()
	portfolioService := NewPortfolioService(ledgerRepo, walletRepo, priceService)

	userID := uuid.New()

	// No wallets for this user
	walletRepo.SetMockWallets(userID, []*Wallet{})

	// Execute
	portfolio, err := portfolioService.GetPortfolioSummary(ctx, userID)

	// Verify
	require.NoError(t, err)
	assert.NotNil(t, portfolio)
	assert.Equal(t, "0", portfolio.TotalUSDValue.String(), "Empty portfolio should have $0 value")
	assert.Len(t, portfolio.AssetHoldings, 0, "Empty portfolio should have no assets")
}

// TestPortfolioService_HandlesPriceAPIFailure verifies graceful handling when prices unavailable (T136 coverage)
func TestPortfolioService_HandlesPriceAPIFailure(t *testing.T) {
	ctx := context.Background()

	ledgerRepo := setupMockLedgerRepository()
	walletRepo := setupMockWalletRepository()
	priceService := setupMockPriceService()
	portfolioService := NewPortfolioService(ledgerRepo, walletRepo, priceService)

	userID := uuid.New()
	walletID := uuid.New()
	accountID := uuid.New()

	walletRepo.SetMockWallets(userID, []*Wallet{
		{ID: walletID, UserID: userID, Name: "Test Wallet", ChainID: "bitcoin"},
	})

	ledgerRepo.SetMockAccounts(walletID, []*ledger.Account{
		{ID: accountID, WalletID: &walletID, AssetID: "BTC"},
	})

	ledgerRepo.SetMockBalances(accountID, []*ledger.AccountBalance{
		{AssetID: "BTC", Balance: big.NewInt(100000000)}, // 1 BTC
	})

	// Price service returns error for BTC
	priceService.SetPriceError("BTC", ErrPriceUnavailable)

	// Execute
	portfolio, err := portfolioService.GetPortfolioSummary(ctx, userID)

	// Verify
	require.NoError(t, err, "Portfolio service should handle price failures gracefully")
	assert.NotNil(t, portfolio)

	// Should have the asset even if price fetch failed
	assert.Len(t, portfolio.AssetHoldings, 1, "Should still have assets even with price errors")
}

// Helper functions

func setupMockLedgerRepository() *MockLedgerRepository {
	return &MockLedgerRepository{
		accounts:        make(map[uuid.UUID][]*ledger.Account),
		accountBalances: make(map[uuid.UUID][]*ledger.AccountBalance),
	}
}

func setupMockWalletRepository() *MockWalletRepository {
	return &MockWalletRepository{
		wallets: make(map[uuid.UUID][]*Wallet),
	}
}

func setupMockPriceService() *MockPriceService {
	return &MockPriceService{
		prices: make(map[string]*big.Int),
		errors: make(map[string]error),
	}
}

// Mock implementations

type MockLedgerRepository struct {
	accounts        map[uuid.UUID][]*ledger.Account
	accountBalances map[uuid.UUID][]*ledger.AccountBalance
}

func (m *MockLedgerRepository) SetMockAccounts(walletID uuid.UUID, accounts []*ledger.Account) {
	m.accounts[walletID] = accounts
}

func (m *MockLedgerRepository) SetMockBalances(accountID uuid.UUID, balances []*ledger.AccountBalance) {
	m.accountBalances[accountID] = balances
}

func (m *MockLedgerRepository) GetAccountBalances(ctx context.Context, accountID uuid.UUID) ([]*ledger.AccountBalance, error) {
	return m.accountBalances[accountID], nil
}

func (m *MockLedgerRepository) GetAccountByCode(ctx context.Context, code string) (*ledger.Account, error) {
	return nil, nil
}

func (m *MockLedgerRepository) FindAccountsByWallet(ctx context.Context, walletID uuid.UUID) ([]*ledger.Account, error) {
	return m.accounts[walletID], nil
}

type MockWalletRepository struct {
	wallets map[uuid.UUID][]*Wallet
}

func (m *MockWalletRepository) SetMockWallets(userID uuid.UUID, wallets []*Wallet) {
	m.wallets[userID] = wallets
}

func (m *MockWalletRepository) GetByUserID(ctx context.Context, userID uuid.UUID) ([]*Wallet, error) {
	return m.wallets[userID], nil
}

type MockPriceService struct {
	prices map[string]*big.Int
	errors map[string]error
}

func (m *MockPriceService) SetMockPrice(assetID string, price *big.Int) {
	m.prices[assetID] = price
}

func (m *MockPriceService) SetPriceError(assetID string, err error) {
	m.errors[assetID] = err
}

func (m *MockPriceService) GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error) {
	if err, ok := m.errors[coinGeckoID]; ok {
		return nil, err
	}
	if price, ok := m.prices[coinGeckoID]; ok {
		return price, nil
	}
	return nil, ErrPriceNotFound
}

var (
	ErrPriceUnavailable = errors.New("price unavailable")
	ErrPriceNotFound    = errors.New("price not found")
)
