package portfolio

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// Wallet represents a wallet entity for portfolio calculations
type Wallet struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Name   string
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

// PriceService defines the interface for price fetching.
// PortfolioPriceAdapter implements this by resolving symbols to CoinGecko IDs.
type PriceService interface {
	GetPriceBySymbol(ctx context.Context, symbol string) (*big.Int, error)
}

// WACProvider supplies weighted-average-cost data for portfolio enrichment.
type WACProvider interface {
	GetWAC(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID) ([]WACPosition, error)
}

// WACPosition represents a single WAC data point (per-chain or aggregated).
type WACPosition struct {
	WalletID        uuid.UUID
	AccountID       uuid.UUID
	ChainID         string
	Asset           string
	TotalQuantity   *big.Int
	WeightedAvgCost *big.Int
	IsAggregated    bool
}

// HoldingGroup represents a single asset across all chains in a wallet.
type HoldingGroup struct {
	AssetID       string          `json:"asset_id"`
	TotalAmount   *big.Int        `json:"total_amount"`
	TotalUSDValue *big.Int        `json:"total_usd_value"`
	Price         *big.Int        `json:"price"`
	AggregatedWAC *big.Int        `json:"aggregated_wac"` // nullable
	Decimals      int             `json:"decimals"`
	Chains        []ChainHolding  `json:"chains"`
}

// ChainHolding represents one asset on one chain within a wallet.
type ChainHolding struct {
	ChainID  string   `json:"chain_id"`
	Amount   *big.Int `json:"amount"`
	USDValue *big.Int `json:"usd_value"`
	WAC      *big.Int `json:"wac"` // nullable, per-chain WAC
}

// PortfolioService aggregates portfolio data from the ledger
type PortfolioService struct {
	ledgerRepo   LedgerRepository
	walletRepo   WalletRepository
	priceService PriceService
	wacProvider  WACProvider              // nilable — WAC enrichment is optional
	resolver     *money.DecimalResolver  // nilable — falls back to money.GetDecimals
}

// NewPortfolioService creates a new portfolio service
func NewPortfolioService(
	ledgerRepo LedgerRepository,
	walletRepo WalletRepository,
	priceService PriceService,
	wacProvider WACProvider,
	resolver *money.DecimalResolver,
) *PortfolioService {
	return &PortfolioService{
		ledgerRepo:   ledgerRepo,
		walletRepo:   walletRepo,
		priceService: priceService,
		wacProvider:  wacProvider,
		resolver:     resolver,
	}
}

// AssetHolding represents a single asset holding across all wallets
type AssetHolding struct {
	AssetID      string   `json:"asset_id"`
	TotalAmount  *big.Int `json:"total_amount"`  // Total amount in base units
	USDValue     *big.Int `json:"usd_value"`     // Current USD value (scaled by 10^8)
	CurrentPrice *big.Int `json:"current_price"` // Current price per unit (scaled by 10^8)
	Decimals     int      `json:"decimals"`      // Asset decimal places for display conversion
}

// WalletBalance represents balance for a single wallet
type WalletBalance struct {
	WalletID   uuid.UUID      `json:"wallet_id"`
	WalletName string         `json:"wallet_name"`
	Assets     []AssetBalance `json:"assets"`
	Holdings   []HoldingGroup `json:"holdings"` // Pre-grouped by asset with WAC
	TotalUSD   *big.Int       `json:"total_usd"`
}

// AssetBalance represents balance for a single asset in a wallet
type AssetBalance struct {
	AssetID  string   `json:"asset_id"`
	ChainID  string   `json:"chain_id,omitempty"` // Zerion chain name, e.g. "ethereum", "base"
	Amount   *big.Int `json:"amount"`             // Amount in base units
	USDValue *big.Int `json:"usd_value"`          // USD value (scaled by 10^8)
	Price    *big.Int `json:"price"`              // Price per unit (scaled by 10^8)
	Decimals int      `json:"decimals"`           // Asset decimal places for display conversion
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

	// walletAssetEntry tracks amount and chain for a wallet+asset+chain combination
	type walletAssetEntry struct {
		AssetID string
		ChainID string
		Amount  *big.Int
	}

	// Aggregate balances by asset (cross-wallet) and by wallet+asset+chain
	assetTotals := make(map[string]*big.Int)                        // assetID -> total amount
	walletAssets := make(map[uuid.UUID]map[string]*walletAssetEntry) // walletID -> "assetID:chainID" -> entry

	for _, account := range accounts {
		if account.WalletID == nil {
			continue // Skip non-wallet accounts
		}

		chainID := ""
		if account.ChainID != nil {
			chainID = *account.ChainID
		}

		// Get balances for this account
		balances, err := s.ledgerRepo.GetAccountBalances(ctx, account.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get account balances: %w", err)
		}

		for _, balance := range balances {
			// Add to asset totals (aggregated across all chains for portfolio overview)
			if _, exists := assetTotals[balance.AssetID]; !exists {
				assetTotals[balance.AssetID] = big.NewInt(0)
			}
			assetTotals[balance.AssetID].Add(assetTotals[balance.AssetID], balance.Balance)

			// Add to wallet-specific tracking keyed by assetID:chainID
			key := balance.AssetID + ":" + chainID
			if _, exists := walletAssets[*account.WalletID]; !exists {
				walletAssets[*account.WalletID] = make(map[string]*walletAssetEntry)
			}
			if _, exists := walletAssets[*account.WalletID][key]; !exists {
				walletAssets[*account.WalletID][key] = &walletAssetEntry{
					AssetID: balance.AssetID,
					ChainID: chainID,
					Amount:  big.NewInt(0),
				}
			}
			walletAssets[*account.WalletID][key].Amount.Add(
				walletAssets[*account.WalletID][key].Amount,
				balance.Balance,
			)
		}
	}

	// Fetch current prices for all assets and calculate USD values
	assetHoldings := make([]AssetHolding, 0, len(assetTotals))
	totalUSD := big.NewInt(0)
	prices := make(map[string]*big.Int) // cache prices for wallet balance calculation

	for assetID, amount := range assetTotals {
		// Skip if balance is zero
		if amount.Cmp(big.NewInt(0)) == 0 {
			continue
		}

		// Get current price (adapter resolves symbol → CoinGecko ID)
		price, err := s.priceService.GetPriceBySymbol(ctx, assetID)
		if err != nil {
			price = big.NewInt(0)
		}
		prices[assetID] = price

		// Calculate USD value: (amount * price) / 10^decimals
		decimals := s.resolveDecimals(ctx, assetID, "")
		usdValue := money.CalcUSDValue(amount, price, decimals)

		assetHoldings = append(assetHoldings, AssetHolding{
			AssetID:      assetID,
			TotalAmount:  new(big.Int).Set(amount),
			USDValue:     usdValue,
			CurrentPrice: price,
			Decimals:     decimals,
		})

		totalUSD.Add(totalUSD, usdValue)
	}

	// Build wallet balances from walletAssets map
	walletBalances := make([]WalletBalance, 0)
	for _, w := range wallets {
		entries, exists := walletAssets[w.ID]
		if !exists {
			continue
		}
		walletTotalUSD := big.NewInt(0)
		assetBalances := make([]AssetBalance, 0)
		for _, entry := range entries {
			if entry.Amount.Sign() == 0 {
				continue
			}
			price := prices[entry.AssetID]
			if price == nil {
				price = big.NewInt(0)
			}
			decimals := s.resolveDecimals(ctx, entry.AssetID, entry.ChainID)
			usdValue := money.CalcUSDValue(entry.Amount, price, decimals)
			walletTotalUSD.Add(walletTotalUSD, usdValue)
			assetBalances = append(assetBalances, AssetBalance{
				AssetID:  entry.AssetID,
				ChainID:  entry.ChainID,
				Amount:   new(big.Int).Set(entry.Amount),
				USDValue: usdValue,
				Price:    new(big.Int).Set(price),
				Decimals: decimals,
			})
		}
		if len(assetBalances) == 0 {
			continue
		}
		walletBalances = append(walletBalances, WalletBalance{
			WalletID:   w.ID,
			WalletName: w.Name,
			Assets:     assetBalances,
			TotalUSD:   walletTotalUSD,
		})
	}

	// Enrich walletBalances with pre-grouped Holdings + WAC
	for i := range walletBalances {
		wb := &walletBalances[i]
		wb.Holdings = s.buildHoldings(ctx, userID, wb)
	}

	summary := &PortfolioSummary{
		TotalUSDValue:  totalUSD,
		TotalAssets:    len(assetHoldings),
		AssetHoldings:  assetHoldings,
		WalletBalances: walletBalances,
		LastUpdated:    "", // Will be set by handler
	}

	return summary, nil
}

// buildHoldings groups a wallet's flat Assets into HoldingGroups by asset_id,
// enriches with WAC data from the provider, and sorts by value descending.
func (s *PortfolioService) buildHoldings(ctx context.Context, userID uuid.UUID, wb *WalletBalance) []HoldingGroup {
	// Group assets by asset_id
	type groupEntry struct {
		assetID  string
		total    *big.Int
		value    *big.Int
		price    *big.Int
		decimals int
		chains   []ChainHolding
	}
	groupMap := make(map[string]*groupEntry)
	var order []string // preserve insertion order

	for _, ab := range wb.Assets {
		g, ok := groupMap[ab.AssetID]
		if !ok {
			g = &groupEntry{
				assetID:  ab.AssetID,
				total:    new(big.Int),
				value:    new(big.Int),
				price:    new(big.Int).Set(ab.Price),
				decimals: ab.Decimals,
			}
			groupMap[ab.AssetID] = g
			order = append(order, ab.AssetID)
		}
		g.total.Add(g.total, ab.Amount)
		g.value.Add(g.value, ab.USDValue)

		if ab.ChainID != "" {
			g.chains = append(g.chains, ChainHolding{
				ChainID:  ab.ChainID,
				Amount:   new(big.Int).Set(ab.Amount),
				USDValue: new(big.Int).Set(ab.USDValue),
			})
		}
	}

	// Fetch WAC data if provider is available
	var wacPositions []WACPosition
	if s.wacProvider != nil {
		wID := wb.WalletID
		positions, err := s.wacProvider.GetWAC(ctx, userID, &wID)
		if err == nil {
			wacPositions = positions
		}
	}

	// Build WAC lookup maps
	type wacKey struct {
		asset   string
		chainID string
	}
	aggWACMap := make(map[string]*big.Int)   // asset → aggregated WAC
	chainWACMap := make(map[wacKey]*big.Int)  // (asset, chainID) → per-chain WAC

	for _, p := range wacPositions {
		if p.IsAggregated {
			aggWACMap[p.Asset] = p.WeightedAvgCost
		} else if p.ChainID != "" {
			chainWACMap[wacKey{p.Asset, p.ChainID}] = p.WeightedAvgCost
		}
	}

	// Build final HoldingGroups
	holdings := make([]HoldingGroup, 0, len(order))
	for _, assetID := range order {
		g := groupMap[assetID]

		// Enrich chains with WAC
		for i := range g.chains {
			if wac, ok := chainWACMap[wacKey{assetID, g.chains[i].ChainID}]; ok {
				g.chains[i].WAC = wac
			}
		}

		hg := HoldingGroup{
			AssetID:       assetID,
			TotalAmount:   g.total,
			TotalUSDValue: g.value,
			Price:         g.price,
			Decimals:      g.decimals,
			Chains:        g.chains,
		}

		if wac, ok := aggWACMap[assetID]; ok {
			hg.AggregatedWAC = wac
		}

		holdings = append(holdings, hg)
	}

	// Sort by total value descending
	sort.Slice(holdings, func(i, j int) bool {
		return holdings[i].TotalUSDValue.Cmp(holdings[j].TotalUSDValue) > 0
	})

	return holdings
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

			// Get current price (adapter resolves symbol → CoinGecko ID)
			price, err := s.priceService.GetPriceBySymbol(ctx, assetID)
			if err != nil {
				price = big.NewInt(0)
			}

			// Calculate USD value with proper decimals
			decimals := s.resolveDecimals(ctx, assetID, "")
			usdValue := money.CalcUSDValue(balance.Balance, price, decimals)

			chainID := ""
			if account.ChainID != nil {
				chainID = *account.ChainID
			}

			assetBalance := AssetBalance{
				AssetID:  assetID,
				ChainID:  chainID,
				Amount:   new(big.Int).Set(balance.Balance),
				USDValue: usdValue,
				Price:    price,
				Decimals: decimals,
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

// resolveDecimals uses the resolver if available, otherwise falls back to hardcoded map.
func (s *PortfolioService) resolveDecimals(ctx context.Context, symbol, chainID string) int {
	if s.resolver != nil {
		return s.resolver.Resolve(ctx, symbol, chainID)
	}
	return money.GetDecimals(symbol)
}
