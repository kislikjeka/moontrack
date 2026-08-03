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

// LotStatusCounter returns lot counts grouped by price_status for a user.
// Used to populate PnLIsPartial / lot count fields in PortfolioSummary.
type LotStatusCounter interface {
	CountLotsByPriceStatus(ctx context.Context, userID uuid.UUID) (pending, unpriceable int, err error)
}

// AssetDescription is what presentation needs to know about a registry id.
//
// It is a struct rather than a return tuple because the set of presentation
// attributes grows: #59 needed symbol and decimals, #42 added the contract and
// the ambiguity flag. Each addition to a tuple breaks every implementation and
// every call site, so the tuple would push the whole layer to churn whenever the
// label changes shape.
type AssetDescription struct {
	// Symbol is the ticker to render. Display only — never an identifier.
	Symbol string
	// Decimals scales base-unit amounts. Wrong here misplaces a decimal point.
	Decimals int
	// Contract is the registry's spelling, verbatim, including the `native`
	// sentinel. It qualifies an ambiguous ticker; nothing else reads it.
	Contract string
	// SymbolAmbiguous says Symbol does not name this asset uniquely on its
	// chain, so a client that shows only the ticker would render two different
	// assets identically. See assetregistry.Asset.SymbolAmbiguous.
	SymbolAmbiguous bool
}

// AssetLookup resolves a registry UUID to the attributes the portfolio needs
// for presentation (#59, extended by #42).
//
// The portfolio only ever holds a UUID now — the ledger balances it sums carry
// nothing else — so the display symbol, the scale and the disambiguator all have
// to be looked up rather than assumed. They come from one call because they come
// from one registry row: reading decimals from a ticker table while reading the
// ticker from the registry is how the two used to disagree.
type AssetLookup interface {
	// Describe returns the presentation attributes for a registry id.
	// A miss returns ok=false and the caller degrades rather than guessing.
	Describe(ctx context.Context, assetID uuid.UUID) (desc AssetDescription, ok bool)
}

// WACPosition represents a single WAC data point (per-chain or aggregated).
type WACPosition struct {
	WalletID        uuid.UUID
	AccountID       uuid.UUID
	ChainID         string
	Asset           uuid.UUID
	TotalQuantity   *big.Int
	WeightedAvgCost *big.Int
	IsAggregated    bool
}

// HoldingGroup represents a single asset across all chains in a wallet.
type HoldingGroup struct {
	// AssetID is the registry UUID; AssetSymbol is the ticker to render (#59).
	// Both ship because the frontend needs a stable key AND a human label, and
	// the old single field could not be both.
	AssetID     uuid.UUID `json:"asset_id"`
	AssetSymbol string    `json:"asset_symbol"`
	// AssetContract and SymbolAmbiguous let the client qualify a ticker that
	// does not name the asset uniquely (#42). Both are presentation only.
	AssetContract   string         `json:"asset_contract"`
	SymbolAmbiguous bool           `json:"symbol_ambiguous"`
	TotalAmount     *big.Int       `json:"total_amount"`
	TotalUSDValue   *big.Int       `json:"total_usd_value"`
	Price           *big.Int       `json:"price"`
	AggregatedWAC   *big.Int       `json:"aggregated_wac"` // nullable
	Decimals        int            `json:"decimals"`
	Chains          []ChainHolding `json:"chains"`
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
	ledgerRepo       LedgerRepository
	walletRepo       WalletRepository
	priceService     PriceService
	wacProvider      WACProvider            // nilable — WAC enrichment is optional
	lotStatusCounter LotStatusCounter       // nilable — lot-count enrichment is optional
	resolver         *money.DecimalResolver // nilable — falls back to money.GetDecimals
	assets           AssetLookup            // nilable — see describeAsset
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

// WithAssetLookup attaches a registry-backed asset lookup, enabling display
// symbols and per-asset decimals on every holding (#59).
func (s *PortfolioService) WithAssetLookup(a AssetLookup) *PortfolioService {
	s.assets = a
	return s
}

// WithLotStatusCounter attaches a LotStatusCounter to the service, enabling
// PnLIsPartial / pending and unpriceable lot count fields in PortfolioSummary.
func (s *PortfolioService) WithLotStatusCounter(c LotStatusCounter) *PortfolioService {
	s.lotStatusCounter = c
	return s
}

// AssetHolding represents a single asset holding across all wallets
type AssetHolding struct {
	AssetID     uuid.UUID `json:"asset_id"`
	AssetSymbol string    `json:"asset_symbol"` // display only — see HoldingGroup
	// ChainID scopes the holding. Holdings are per (asset, chain) rather than
	// per symbol because base-unit amounts are only comparable within one
	// chain's decimals — the same ticker can carry different decimals on
	// different chains.
	ChainID string `json:"chain_id"`
	// AssetContract and SymbolAmbiguous qualify an ambiguous ticker — see
	// HoldingGroup.
	AssetContract   string   `json:"asset_contract"`
	SymbolAmbiguous bool     `json:"symbol_ambiguous"`
	TotalAmount     *big.Int `json:"total_amount"`  // Total amount in base units
	USDValue        *big.Int `json:"usd_value"`     // Current USD value (scaled by 10^8)
	CurrentPrice    *big.Int `json:"current_price"` // Current price per unit (scaled by 10^8)
	Decimals        int      `json:"decimals"`      // Asset decimal places for display conversion
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
	AssetID     uuid.UUID `json:"asset_id"`
	AssetSymbol string    `json:"asset_symbol"`       // display only — see HoldingGroup
	ChainID     string    `json:"chain_id,omitempty"` // Zerion chain name, e.g. "ethereum", "base"
	// AssetContract and SymbolAmbiguous qualify an ambiguous ticker — see
	// HoldingGroup.
	AssetContract   string   `json:"asset_contract"`
	SymbolAmbiguous bool     `json:"symbol_ambiguous"`
	Amount          *big.Int `json:"amount"`    // Amount in base units
	USDValue        *big.Int `json:"usd_value"` // USD value (scaled by 10^8)
	Price           *big.Int `json:"price"`     // Price per unit (scaled by 10^8)
	Decimals        int      `json:"decimals"`  // Asset decimal places for display conversion
}

// PortfolioSummary represents the complete portfolio overview
type PortfolioSummary struct {
	TotalUSDValue       *big.Int        `json:"total_usd_value"`       // Total portfolio value in USD (scaled by 10^8)
	TotalAssets         int             `json:"total_assets"`          // Number of unique assets
	AssetHoldings       []AssetHolding  `json:"asset_holdings"`        // Aggregated holdings by asset
	WalletBalances      []WalletBalance `json:"wallet_balances"`       // Balances per wallet
	LastUpdated         string          `json:"last_updated"`          // ISO 8601 timestamp
	PnLIsPartial        bool            `json:"pnl_is_partial"`        // True when ≥1 lot has pending price — PnL figures are incomplete
	PendingLotCount     int             `json:"pending_lot_count"`     // Number of lots still awaiting price resolution
	UnpriceableLotCount int             `json:"unpriceable_lot_count"` // Number of lots that could not be priced
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
		AssetID uuid.UUID
		ChainID string
		Amount  *big.Int
	}

	// Aggregate balances by asset+chain (cross-wallet) and by wallet+asset+chain.
	//
	// The cross-wallet totals are keyed by asset AND chain, never by asset
	// alone: a balance is a raw base-unit integer whose scale is 10^decimals,
	// and decimals are a property of the (asset, chain) contract, not of the
	// symbol. The same ticker legitimately differs across chains — USDC is 6
	// decimals on Ethereum and Base but 18 on BNB Chain — so summing raw
	// amounts under one symbol adds quantities on incompatible scales and
	// inflates the holding by up to 10^12.
	assetTotals := make(map[string]*big.Int)                         // "assetID:chainID" -> total amount
	assetTotalKeys := make(map[string]walletAssetEntry)              // "assetID:chainID" -> its asset/chain pair
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
			// Add to wallet-specific tracking keyed by assetID:chainID
			// The map key is the UUID's string form. It is a key, not an
			// identifier anyone reads — the AssetID beside it stays typed.
			key := balance.AssetID.String() + ":" + chainID

			// Add to cross-wallet totals under the same asset+chain key, so the
			// decimals used to value it are the ones this balance is scaled in.
			if _, exists := assetTotals[key]; !exists {
				assetTotals[key] = big.NewInt(0)
				assetTotalKeys[key] = walletAssetEntry{AssetID: balance.AssetID, ChainID: chainID}
			}
			assetTotals[key].Add(assetTotals[key], balance.Balance)

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
	prices := make(map[uuid.UUID]*big.Int) // cache prices per registry asset

	for key, amount := range assetTotals {
		// Skip if balance is zero
		if amount.Cmp(big.NewInt(0)) == 0 {
			continue
		}

		pair := assetTotalKeys[key]
		assetID := pair.AssetID

		// One registry read gives the label, the scale and the disambiguator
		// (#59, #42).
		desc := s.describeAsset(ctx, assetID, pair.ChainID)
		symbol, decimals := desc.Symbol, desc.Decimals

		// Price is still keyed on the ticker: the only price source wired here
		// resolves a symbol to a CoinGecko slug. An asset with no resolvable
		// symbol therefore values at zero, which is this adapter's existing
		// convention for "no price" (see PortfolioPriceAdapter). Valuing by
		// registry identity is #42's registry-keyed price pipeline, not a
		// ticker guess reinstated here.
		price, err := s.priceService.GetPriceBySymbol(ctx, symbol)
		if err != nil {
			price = big.NewInt(0)
		}
		prices[assetID] = price

		// Calculate USD value: (amount * price) / 10^decimals, with the
		// decimals of THIS chain's contract — see the keying note above.
		usdValue := money.CalcUSDValue(amount, price, decimals)

		assetHoldings = append(assetHoldings, AssetHolding{
			AssetID:         assetID,
			AssetSymbol:     symbol,
			ChainID:         pair.ChainID,
			AssetContract:   desc.Contract,
			SymbolAmbiguous: desc.SymbolAmbiguous,
			TotalAmount:     new(big.Int).Set(amount),
			USDValue:        usdValue,
			CurrentPrice:    price,
			Decimals:        decimals,
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
			desc := s.describeAsset(ctx, entry.AssetID, entry.ChainID)
			usdValue := money.CalcUSDValue(entry.Amount, price, desc.Decimals)
			walletTotalUSD.Add(walletTotalUSD, usdValue)
			assetBalances = append(assetBalances, AssetBalance{
				AssetID:         entry.AssetID,
				AssetSymbol:     desc.Symbol,
				ChainID:         entry.ChainID,
				AssetContract:   desc.Contract,
				SymbolAmbiguous: desc.SymbolAmbiguous,
				Amount:          new(big.Int).Set(entry.Amount),
				USDValue:        usdValue,
				Price:           new(big.Int).Set(price),
				Decimals:        desc.Decimals,
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

	// Populate lot price-status counts when a counter is available.
	var pendingCount, unpriceableCount int
	if s.lotStatusCounter != nil {
		pendingCount, unpriceableCount, _ = s.lotStatusCounter.CountLotsByPriceStatus(ctx, userID)
		// Errors are non-fatal: we still return a valid (partial) portfolio.
	}

	summary := &PortfolioSummary{
		TotalUSDValue:       totalUSD,
		TotalAssets:         len(assetHoldings),
		AssetHoldings:       assetHoldings,
		WalletBalances:      walletBalances,
		LastUpdated:         "", // Will be set by handler
		PnLIsPartial:        pendingCount > 0,
		PendingLotCount:     pendingCount,
		UnpriceableLotCount: unpriceableCount,
	}

	return summary, nil
}

// buildHoldings groups a wallet's flat Assets into HoldingGroups by asset_id,
// enriches with WAC data from the provider, and sorts by value descending.
func (s *PortfolioService) buildHoldings(ctx context.Context, userID uuid.UUID, wb *WalletBalance) []HoldingGroup {
	// Group assets by asset_id
	type groupEntry struct {
		assetID uuid.UUID
		symbol  string
		// contract and ambiguous are properties of the registry row, not of a
		// chain, so taking them from the first AssetBalance of the group is
		// exact: every balance under one asset_id resolves the same row. The
		// group is keyed on asset_id precisely so two contracts sharing a
		// ticker cannot collapse into one row here.
		contract  string
		ambiguous bool
		total     *big.Int
		value     *big.Int
		price     *big.Int
		decimals  int
		chains    []ChainHolding
	}
	groupMap := make(map[uuid.UUID]*groupEntry)
	var order []uuid.UUID // preserve insertion order

	for _, ab := range wb.Assets {
		g, ok := groupMap[ab.AssetID]
		if !ok {
			g = &groupEntry{
				assetID:   ab.AssetID,
				symbol:    ab.AssetSymbol,
				contract:  ab.AssetContract,
				ambiguous: ab.SymbolAmbiguous,
				total:     new(big.Int),
				value:     new(big.Int),
				price:     new(big.Int).Set(ab.Price),
				decimals:  ab.Decimals,
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
		asset   uuid.UUID
		chainID string
	}
	aggWACMap := make(map[uuid.UUID]*big.Int) // asset → aggregated WAC
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
			AssetID:         assetID,
			AssetSymbol:     g.symbol,
			AssetContract:   g.contract,
			SymbolAmbiguous: g.ambiguous,
			TotalAmount:     g.total,
			TotalUSDValue:   g.value,
			Price:           g.price,
			Decimals:        g.decimals,
			Chains:          g.chains,
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

// describeAsset resolves a registry id to its presentation attributes.
//
// With a lookup wired, all of them come from the one registry row the id names.
// Without one — the wiring is optional so tests can construct a bare service —
// the symbol is left EMPTY rather than filled with the UUID's string form: a
// UUID where a ticker belongs is worse than a blank, because it reads as data.
// The scale then falls back to the symbol-keyed resolver, which with no symbol
// yields the hardcoded default.
//
// An unresolvable asset is reported as unambiguous. The flag answers "would the
// ticker alone mislead", and a holding with no ticker at all shows nothing to be
// misled by; raising it would make the UI qualify a blank label with a contract
// it also does not have.
func (s *PortfolioService) describeAsset(ctx context.Context, assetID uuid.UUID, chainID string) AssetDescription {
	if s.assets != nil {
		if desc, ok := s.assets.Describe(ctx, assetID); ok {
			return desc
		}
	}
	return AssetDescription{Decimals: s.resolveDecimals(ctx, "", chainID)}
}

// resolveDecimals uses the resolver if available, otherwise falls back to hardcoded map.
func (s *PortfolioService) resolveDecimals(ctx context.Context, symbol, chainID string) int {
	if s.resolver != nil {
		return s.resolver.Resolve(ctx, symbol, chainID)
	}
	return money.GetDecimals(symbol)
}
