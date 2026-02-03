package asset

import (
	"context"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// Repository defines the interface for asset persistence operations
type Repository interface {
	// GetByID retrieves an asset by its UUID
	GetByID(ctx context.Context, id uuid.UUID) (*Asset, error)

	// GetBySymbol retrieves an asset by symbol, optionally filtered by chain
	// Returns ErrAmbiguousSymbol if symbol exists on multiple chains and chainID is nil
	GetBySymbol(ctx context.Context, symbol string, chainID *string) (*Asset, error)

	// GetByCoinGeckoID retrieves an asset by its CoinGecko ID
	GetByCoinGeckoID(ctx context.Context, coinGeckoID string) (*Asset, error)

	// GetAllBySymbol retrieves all assets matching a symbol across all chains
	GetAllBySymbol(ctx context.Context, symbol string) ([]Asset, error)

	// Create creates a new asset in the database
	Create(ctx context.Context, asset *Asset) error

	// Search searches for assets by query string (matches symbol or name)
	Search(ctx context.Context, query string, limit int) ([]Asset, error)

	// GetActiveAssets retrieves all active assets
	GetActiveAssets(ctx context.Context) ([]Asset, error)

	// GetByChain retrieves all assets on a specific chain
	GetByChain(ctx context.Context, chainID string) ([]Asset, error)
}

// PriceRepository defines the interface for price history persistence operations
type PriceRepository interface {
	// RecordPrice inserts or updates a price record
	RecordPrice(ctx context.Context, price *PricePoint) error

	// GetCurrentPrice retrieves the most recent price for an asset
	GetCurrentPrice(ctx context.Context, assetID uuid.UUID) (*PricePoint, error)

	// GetPriceAt retrieves the price at or before a specific time
	GetPriceAt(ctx context.Context, assetID uuid.UUID, at time.Time) (*PricePoint, error)

	// GetPriceHistory retrieves price history for a time range with specified interval
	GetPriceHistory(ctx context.Context, query *PriceHistoryQuery) ([]PricePoint, error)

	// GetOHLCV retrieves OHLCV data for a time range
	GetOHLCV(ctx context.Context, query *PriceHistoryQuery) ([]OHLCV, error)

	// GetRecentPrice retrieves price within the last N minutes (for cache fallback)
	GetRecentPrice(ctx context.Context, assetID uuid.UUID, maxAge time.Duration) (*PricePoint, error)
}

// PriceCache defines the interface for price caching operations
type PriceCache interface {
	// Get retrieves a cached price for an asset
	Get(ctx context.Context, assetID string) (*big.Int, bool, error)

	// Set stores a price in the cache
	Set(ctx context.Context, assetID string, price *big.Int, source string) error

	// SetStale stores a price in the stale cache (24-hour TTL for fallback)
	SetStale(ctx context.Context, assetID string, price *big.Int, source string) error

	// GetStale retrieves a price from the stale cache (fallback when API fails)
	GetStale(ctx context.Context, assetID string) (*big.Int, bool, error)

	// GetMultiple retrieves cached prices for multiple assets
	GetMultiple(ctx context.Context, assetIDs []string) (map[string]*big.Int, error)
}

// PriceProvider defines the interface for external price providers (e.g., CoinGecko)
type PriceProvider interface {
	// GetCurrentPrices fetches current prices for multiple assets
	GetCurrentPrices(ctx context.Context, coinGeckoIDs []string) (map[string]*big.Int, error)

	// GetHistoricalPrice fetches the price at a specific date
	GetHistoricalPrice(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error)

	// SearchCoins searches for coins matching a query
	SearchCoins(ctx context.Context, query string) ([]CoinSearchResult, error)
}

// CoinSearchResult represents a result from coin search
type CoinSearchResult struct {
	ID            string
	Symbol        string
	Name          string
	MarketCapRank int
}
