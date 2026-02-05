package portfolio

import (
	"context"
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
)

// Wallet represents a wallet entity for portfolio calculations
type Wallet struct {
	ID      uuid.UUID
	UserID  uuid.UUID
	Name    string
	ChainID int64
}

// WalletRepository defines the interface for wallet operations
type WalletRepository interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) ([]*Wallet, error)
}

// LedgerRepository defines the interface for ledger operations
type LedgerRepository interface {
	GetAccountBalances(ctx context.Context, accountID uuid.UUID) ([]*ledger.AccountBalance, error)
	GetAccountByCode(ctx context.Context, code string) (*ledger.Account, error)
	FindAccountsByWallet(ctx context.Context, walletID uuid.UUID) ([]*ledger.Account, error)
}

// PriceService defines the interface for price fetching
// RegistryService implements this via GetCurrentPriceByCoinGeckoID
type PriceService interface {
	GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error)
}

// PortfolioService aggregates portfolio data from the ledger
type PortfolioService struct {
	ledgerRepo   LedgerRepository
	walletRepo   WalletRepository
	priceService PriceService
}

// NewPortfolioService creates a new portfolio service
func NewPortfolioService(
	ledgerRepo LedgerRepository,
	walletRepo WalletRepository,
	priceService PriceService,
) *PortfolioService {
	return &PortfolioService{
		ledgerRepo:   ledgerRepo,
		walletRepo:   walletRepo,
		priceService: priceService,
	}
}

// AssetHolding represents a single asset holding across all wallets
type AssetHolding struct {
	AssetID      string   `json:"asset_id"`
	TotalAmount  *big.Int `json:"total_amount"`  // Total amount in base units
	USDValue     *big.Int `json:"usd_value"`     // Current USD value (scaled by 10^8)
	CurrentPrice *big.Int `json:"current_price"` // Current price per unit (scaled by 10^8)
}

// WalletBalance represents balance for a single wallet
type WalletBalance struct {
	WalletID   uuid.UUID      `json:"wallet_id"`
	WalletName string         `json:"wallet_name"`
	ChainID    string         `json:"chain_id"`
	Assets     []AssetBalance `json:"assets"`
	TotalUSD   *big.Int       `json:"total_usd"` // Total USD value of all assets in this wallet
}

// AssetBalance represents balance for a single asset in a wallet
type AssetBalance struct {
	AssetID  string   `json:"asset_id"`
	Amount   *big.Int `json:"amount"`    // Amount in base units
	USDValue *big.Int `json:"usd_value"` // USD value (scaled by 10^8)
	Price    *big.Int `json:"price"`     // Price per unit (scaled by 10^8)
}

// PortfolioSummary represents the complete portfolio overview
type PortfolioSummary struct {
	TotalUSDValue  *big.Int        `json:"total_usd_value"` // Total portfolio value in USD (scaled by 10^8)
	TotalAssets    int             `json:"total_assets"`    // Number of unique assets
	AssetHoldings  []AssetHolding  `json:"asset_holdings"`  // Aggregated holdings by asset
	WalletBalances []WalletBalance `json:"wallet_balances"` // Balances per wallet
	LastUpdated    string          `json:"last_updated"`    // ISO 8601 timestamp
}

// GetPortfolioSummary returns the complete portfolio summary for a user
func (s *PortfolioService) GetPortfolioSummary(ctx context.Context, userID uuid.UUID) (*PortfolioSummary, error) {
	// Get all wallets for the user
	wallets, err := s.walletRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user wallets: %w", err)
	}

	// Get all accounts for these wallets
	var accounts []*ledger.Account
	for _, wallet := range wallets {
		walletAccounts, err := s.ledgerRepo.FindAccountsByWallet(ctx, wallet.ID)
		if err != nil {
			continue
		}
		accounts = append(accounts, walletAccounts...)
	}

	// Aggregate balances by asset
	assetTotals := make(map[string]*big.Int)                // assetID -> total amount
	walletAssets := make(map[uuid.UUID]map[string]*big.Int) // walletID -> assetID -> amount

	for _, account := range accounts {
		if account.WalletID == nil {
			continue // Skip non-wallet accounts
		}

		// Get balances for this account
		balances, err := s.ledgerRepo.GetAccountBalances(ctx, account.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get account balances: %w", err)
		}

		for _, balance := range balances {
			// Add to asset totals
			if _, exists := assetTotals[balance.AssetID]; !exists {
				assetTotals[balance.AssetID] = big.NewInt(0)
			}
			assetTotals[balance.AssetID].Add(assetTotals[balance.AssetID], balance.Balance)

			// Add to wallet-specific tracking
			if _, exists := walletAssets[*account.WalletID]; !exists {
				walletAssets[*account.WalletID] = make(map[string]*big.Int)
			}
			if _, exists := walletAssets[*account.WalletID][balance.AssetID]; !exists {
				walletAssets[*account.WalletID][balance.AssetID] = big.NewInt(0)
			}
			walletAssets[*account.WalletID][balance.AssetID].Add(
				walletAssets[*account.WalletID][balance.AssetID],
				balance.Balance,
			)
		}
	}

	// Fetch current prices for all assets and calculate USD values
	assetHoldings := make([]AssetHolding, 0, len(assetTotals))
	totalUSD := big.NewInt(0)

	for assetID, amount := range assetTotals {
		// Skip if balance is zero
		if amount.Cmp(big.NewInt(0)) == 0 {
			continue
		}

		// Get current price
		price, err := s.priceService.GetCurrentPriceByCoinGeckoID(ctx, assetID)
		if err != nil {
			// If price fetch fails, use zero (or could use cached price)
			price = big.NewInt(0)
		}

		// Calculate USD value: (amount * price) / 10^8
		// Price is already scaled by 10^8, amount is in base units
		// For ETH: amount in wei, price in USD*10^8 per ETH
		// Result: (wei * (USD*10^8/ETH)) / 10^18 = USD*10^8
		usdValue := new(big.Int).Mul(amount, price)
		// Note: This calculation depends on asset's decimal places
		// For now, we'll store raw calculation - proper implementation needs asset metadata

		assetHoldings = append(assetHoldings, AssetHolding{
			AssetID:      assetID,
			TotalAmount:  new(big.Int).Set(amount),
			USDValue:     usdValue,
			CurrentPrice: price,
		})

		totalUSD.Add(totalUSD, usdValue)
	}

	// Build wallet balances (simplified - would need wallet details from WalletRepository)
	walletBalances := make([]WalletBalance, 0)
	// TODO: Fetch wallet details and build WalletBalance structs
	// For now, returning just asset holdings

	summary := &PortfolioSummary{
		TotalUSDValue:  totalUSD,
		TotalAssets:    len(assetHoldings),
		AssetHoldings:  assetHoldings,
		WalletBalances: walletBalances,
		LastUpdated:    "", // Will be set by handler
	}

	return summary, nil
}

// GetAssetBreakdown returns detailed breakdown of a specific asset across all wallets
func (s *PortfolioService) GetAssetBreakdown(ctx context.Context, userID uuid.UUID, assetID string) ([]WalletBalance, error) {
	// Get all wallets for the user
	wallets, err := s.walletRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user wallets: %w", err)
	}

	// Get accounts for each wallet
	var accounts []*ledger.Account
	for _, wallet := range wallets {
		walletAccounts, err := s.ledgerRepo.FindAccountsByWallet(ctx, wallet.ID)
		if err != nil {
			continue
		}
		accounts = append(accounts, walletAccounts...)
	}

	walletBalances := make([]WalletBalance, 0)

	for _, account := range accounts {
		if account.WalletID == nil {
			continue
		}

		// Get balance for this account
		balances, err := s.ledgerRepo.GetAccountBalances(ctx, account.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get account balances: %w", err)
		}

		for _, balance := range balances {
			if balance.AssetID != assetID {
				continue
			}

			// Get current price
			price, err := s.priceService.GetCurrentPriceByCoinGeckoID(ctx, assetID)
			if err != nil {
				price = big.NewInt(0)
			}

			// Calculate USD value
			usdValue := new(big.Int).Mul(balance.Balance, price)

			assetBalance := AssetBalance{
				AssetID:  assetID,
				Amount:   new(big.Int).Set(balance.Balance),
				USDValue: usdValue,
				Price:    price,
			}

			// TODO: Fetch wallet details
			walletBalance := WalletBalance{
				WalletID: *account.WalletID,
				Assets:   []AssetBalance{assetBalance},
				TotalUSD: usdValue,
			}

			walletBalances = append(walletBalances, walletBalance)
		}
	}

	return walletBalances, nil
}
