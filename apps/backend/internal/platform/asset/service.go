package asset

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Service provides unified access to asset registry and pricing
type Service struct {
	repo           Repository
	priceRepo      PriceRepository
	cache          PriceCache
	priceProvider  PriceProvider
	circuitBreaker *CircuitBreaker
}

// NewService creates a new asset service
func NewService(
	repo Repository,
	priceRepo PriceRepository,
	cache PriceCache,
	priceProvider PriceProvider,
) *Service {
	return &Service{
		repo:           repo,
		priceRepo:      priceRepo,
		cache:          cache,
		priceProvider:  priceProvider,
		circuitBreaker: NewCircuitBreaker(3, 5*time.Minute), // 3 failures, 5-minute cooldown
	}
}

// CircuitBreaker implements a simple circuit breaker pattern
type CircuitBreaker struct {
	maxFailures     int
	cooldownPeriod  time.Duration
	failures        int
	lastFailureTime time.Time
	state           CircuitState
	mu              sync.RWMutex
}

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Too many failures, blocking requests
	CircuitHalfOpen                     // Testing if service recovered
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, cooldownPeriod time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:    maxFailures,
		cooldownPeriod: cooldownPeriod,
		state:          CircuitClosed,
	}
}

// CanAttempt returns true if a request can be attempted
func (cb *CircuitBreaker) CanAttempt() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == CircuitClosed {
		return true
	}

	if cb.state == CircuitOpen {
		// Check if cooldown period has passed
		if time.Since(cb.lastFailureTime) > cb.cooldownPeriod {
			return true // Try half-open state
		}
		return false
	}

	// Half-open: allow one attempt
	return true
}

// RecordSuccess records a successful API call
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = CircuitClosed
}

// RecordFailure records a failed API call
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.failures >= cb.maxFailures {
		cb.state = CircuitOpen
	}
}

// StalePriceWarning represents a warning when using stale cached prices
type StalePriceWarning struct {
	Message string
	Price   *big.Int
}

func (w *StalePriceWarning) Error() string {
	return w.Message
}

// ---- Asset Operations ----

// GetAsset retrieves an asset by its UUID
func (s *Service) GetAsset(ctx context.Context, id uuid.UUID) (*Asset, error) {
	return s.repo.GetByID(ctx, id)
}

// GetAssetBySymbol retrieves an asset by symbol, optionally filtered by chain
// Returns ErrAmbiguousSymbol if symbol exists on multiple chains and chainID is nil
func (s *Service) GetAssetBySymbol(ctx context.Context, symbol string, chainID *string) (*Asset, error) {
	return s.repo.GetBySymbol(ctx, symbol, chainID)
}

// GetAssetsBySymbol retrieves all assets matching a symbol across all chains
func (s *Service) GetAssetsBySymbol(ctx context.Context, symbol string) ([]Asset, error) {
	return s.repo.GetAllBySymbol(ctx, symbol)
}

// GetAssetByCoinGeckoID retrieves an asset by its CoinGecko ID
func (s *Service) GetAssetByCoinGeckoID(ctx context.Context, coinGeckoID string) (*Asset, error) {
	return s.repo.GetByCoinGeckoID(ctx, coinGeckoID)
}

// SearchAssets searches for assets by query string
func (s *Service) SearchAssets(ctx context.Context, query string) ([]Asset, error) {
	return s.repo.Search(ctx, query, 20)
}

// CreateAsset creates a new asset in the registry
func (s *Service) CreateAsset(ctx context.Context, asset *Asset) error {
	return s.repo.Create(ctx, asset)
}

// GetActiveAssets retrieves all active assets
func (s *Service) GetActiveAssets(ctx context.Context) ([]Asset, error) {
	return s.repo.GetActiveAssets(ctx)
}

// GetAssetsByChain retrieves all assets on a specific chain
func (s *Service) GetAssetsByChain(ctx context.Context, chainID string) ([]Asset, error) {
	return s.repo.GetByChain(ctx, chainID)
}

// ---- Price Operations ----

// GetCurrentPrice fetches the current USD price for an asset using multi-layer fallback
// Fallback order: Redis cache (60s) → price_history (5min) → CoinGecko API → stale cache (24h)
func (s *Service) GetCurrentPrice(ctx context.Context, assetID uuid.UUID) (*PricePoint, error) {
	// Get asset for CoinGecko ID
	asset, err := s.repo.GetByID(ctx, assetID)
	if err != nil {
		return nil, err
	}

	// Layer 1: Check Redis cache (60s TTL)
	if s.cache != nil {
		price, found, err := s.cache.Get(ctx, asset.CoinGeckoID)
		if err == nil && found {
			return &PricePoint{
				Time:     time.Now(),
				AssetID:  assetID,
				PriceUSD: price,
				Source:   PriceSourceCoinGecko,
				IsStale:  false,
			}, nil
		}
	}

	// Layer 2: Check price_history (last 5 minutes)
	if s.priceRepo != nil {
		pricePoint, err := s.priceRepo.GetRecentPrice(ctx, assetID, 5*time.Minute)
		if err == nil && pricePoint != nil {
			// Update cache
			if s.cache != nil {
				_ = s.cache.Set(ctx, asset.CoinGeckoID, pricePoint.PriceUSD, string(pricePoint.Source))
			}
			return pricePoint, nil
		}
	}

	// Layer 3: Fetch from provider API
	if s.priceProvider != nil {
		prices, err := s.priceProvider.GetCurrentPrices(ctx, []string{asset.CoinGeckoID})
		if err == nil {
			if price, found := prices[asset.CoinGeckoID]; found {
				pricePoint := &PricePoint{
					Time:     time.Now(),
					AssetID:  assetID,
					PriceUSD: price,
					Source:   PriceSourceCoinGecko,
					IsStale:  false,
				}

				// Save to price_history and cache
				if s.priceRepo != nil {
					_ = s.priceRepo.RecordPrice(ctx, pricePoint)
				}
				if s.cache != nil {
					_ = s.cache.Set(ctx, asset.CoinGeckoID, price, "coingecko")
					_ = s.cache.SetStale(ctx, asset.CoinGeckoID, price, "coingecko")
				}

				return pricePoint, nil
			}
		}
	}

	// Layer 4: Fallback to stale cache (24h TTL)
	if s.cache != nil {
		price, found, err := s.cache.GetStale(ctx, asset.CoinGeckoID)
		if err == nil && found {
			return &PricePoint{
				Time:     time.Now(),
				AssetID:  assetID,
				PriceUSD: price,
				Source:   PriceSourceCoinGecko,
				IsStale:  true,
			}, nil
		}
	}

	// Layer 5: No price available
	return nil, ErrPriceUnavailable
}

// GetPriceAt retrieves the price at or before a specific time
func (s *Service) GetPriceAt(ctx context.Context, assetID uuid.UUID, at time.Time) (*PricePoint, error) {
	return s.priceRepo.GetPriceAt(ctx, assetID, at)
}

// GetPriceHistory retrieves price history for a time range with specified interval
func (s *Service) GetPriceHistory(ctx context.Context, assetID uuid.UUID, from, to time.Time, interval PriceInterval) ([]PricePoint, error) {
	query := &PriceHistoryQuery{
		AssetID:  assetID,
		From:     from,
		To:       to,
		Interval: interval,
	}
	return s.priceRepo.GetPriceHistory(ctx, query)
}

// RecordPrice records a price point for an asset
func (s *Service) RecordPrice(ctx context.Context, assetID uuid.UUID, price *big.Int, source PriceSource) error {
	pricePoint := &PricePoint{
		Time:     time.Now(),
		AssetID:  assetID,
		PriceUSD: price,
		Source:   source,
	}
	return s.priceRepo.RecordPrice(ctx, pricePoint)
}

// GetBatchPrices fetches current prices for multiple assets
func (s *Service) GetBatchPrices(ctx context.Context, assetIDs []uuid.UUID) (map[uuid.UUID]*PricePoint, error) {
	if len(assetIDs) == 0 {
		return make(map[uuid.UUID]*PricePoint), nil
	}

	result := make(map[uuid.UUID]*PricePoint)

	// Get all assets to map UUID -> CoinGecko ID
	assetMap := make(map[uuid.UUID]*Asset)
	coinGeckoIDs := make([]string, 0, len(assetIDs))

	for _, id := range assetIDs {
		asset, err := s.repo.GetByID(ctx, id)
		if err != nil {
			continue
		}
		assetMap[id] = asset
		coinGeckoIDs = append(coinGeckoIDs, asset.CoinGeckoID)
	}

	// Try to get from cache first
	if s.cache != nil {
		cached, err := s.cache.GetMultiple(ctx, coinGeckoIDs)
		if err == nil {
			for assetID, asset := range assetMap {
				if price, found := cached[asset.CoinGeckoID]; found {
					result[assetID] = &PricePoint{
						Time:     time.Now(),
						AssetID:  assetID,
						PriceUSD: price,
						Source:   PriceSourceCoinGecko,
						IsStale:  false,
					}
				}
			}
		}
	}

	// Find missing asset IDs
	missingCGIDs := make([]string, 0)
	for _, asset := range assetMap {
		found := false
		for assetID := range result {
			if assetMap[assetID].CoinGeckoID == asset.CoinGeckoID {
				found = true
				break
			}
		}
		if !found {
			missingCGIDs = append(missingCGIDs, asset.CoinGeckoID)
		}
	}

	// Fetch missing prices from provider
	if len(missingCGIDs) > 0 && s.priceProvider != nil {
		prices, err := s.priceProvider.GetCurrentPrices(ctx, missingCGIDs)
		if err == nil {
			for assetID, asset := range assetMap {
				if _, exists := result[assetID]; exists {
					continue
				}
				if price, found := prices[asset.CoinGeckoID]; found {
					pricePoint := &PricePoint{
						Time:     time.Now(),
						AssetID:  assetID,
						PriceUSD: price,
						Source:   PriceSourceCoinGecko,
						IsStale:  false,
					}
					result[assetID] = pricePoint

					// Cache and persist
					if s.cache != nil {
						_ = s.cache.Set(ctx, asset.CoinGeckoID, price, "coingecko")
						_ = s.cache.SetStale(ctx, asset.CoinGeckoID, price, "coingecko")
					}
					if s.priceRepo != nil {
						_ = s.priceRepo.RecordPrice(ctx, pricePoint)
					}
				}
			}
		}
	}

	return result, nil
}

// GetCurrentPriceByCoinGeckoID fetches the current USD price for an asset by CoinGecko ID
// This method is useful for transaction handlers that work with CoinGecko IDs
func (s *Service) GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error) {
	// Layer 1: Check Redis cache (60s TTL)
	if s.cache != nil {
		price, found, err := s.cache.Get(ctx, coinGeckoID)
		if err == nil && found {
			return price, nil
		}
	}

	// Layer 2: Try to get asset and check price_history
	asset, err := s.repo.GetByCoinGeckoID(ctx, coinGeckoID)
	if err == nil && asset != nil && s.priceRepo != nil {
		pricePoint, err := s.priceRepo.GetRecentPrice(ctx, asset.ID, 5*time.Minute)
		if err == nil && pricePoint != nil {
			// Update cache
			if s.cache != nil {
				_ = s.cache.Set(ctx, coinGeckoID, pricePoint.PriceUSD, string(pricePoint.Source))
			}
			return pricePoint.PriceUSD, nil
		}
	}

	// Layer 3: Fetch from provider API (if circuit breaker allows)
	if s.priceProvider != nil && s.circuitBreaker.CanAttempt() {
		prices, err := s.priceProvider.GetCurrentPrices(ctx, []string{coinGeckoID})
		if err == nil {
			if price, found := prices[coinGeckoID]; found {
				// Save to cache
				if s.cache != nil {
					_ = s.cache.Set(ctx, coinGeckoID, price, "coingecko")
					_ = s.cache.SetStale(ctx, coinGeckoID, price, "coingecko")
				}

				// Save to price_history if we have the asset
				if asset != nil && s.priceRepo != nil {
					pricePoint := &PricePoint{
						Time:     time.Now(),
						AssetID:  asset.ID,
						PriceUSD: price,
						Source:   PriceSourceCoinGecko,
					}
					_ = s.priceRepo.RecordPrice(ctx, pricePoint)
				}

				s.circuitBreaker.RecordSuccess()
				return price, nil
			}
		} else {
			s.circuitBreaker.RecordFailure()
		}
	}

	// Layer 4: Fallback to stale cache (24h TTL)
	if s.cache != nil {
		price, found, err := s.cache.GetStale(ctx, coinGeckoID)
		if err == nil && found {
			return price, &StalePriceWarning{
				Message: fmt.Sprintf("Using stale cached price for %s (API unavailable)", coinGeckoID),
				Price:   price,
			}
		}
	}

	return nil, ErrPriceUnavailable
}

// GetHistoricalPriceByCoinGeckoID fetches the USD price for an asset on a specific date
// This method is useful for transaction handlers that work with CoinGecko IDs
func (s *Service) GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error) {
	// Normalize date to midnight UTC
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	// Layer 1: Try to get from price_history if we have the asset
	asset, _ := s.repo.GetByCoinGeckoID(ctx, coinGeckoID)
	if asset != nil && s.priceRepo != nil {
		pricePoint, err := s.priceRepo.GetPriceAt(ctx, asset.ID, date)
		if err == nil && pricePoint != nil {
			return pricePoint.PriceUSD, nil
		}
	}

	// Layer 2: Fetch from provider API (if circuit breaker allows)
	if s.priceProvider == nil || !s.circuitBreaker.CanAttempt() {
		return nil, ErrPriceUnavailable
	}

	price, err := s.priceProvider.GetHistoricalPrice(ctx, coinGeckoID, date)
	if err != nil {
		s.circuitBreaker.RecordFailure()
		return nil, fmt.Errorf("failed to fetch historical price: %w", err)
	}

	// Save to price_history if we have the asset
	if asset != nil && s.priceRepo != nil {
		pricePoint := &PricePoint{
			Time:     date,
			AssetID:  asset.ID,
			PriceUSD: price,
			Source:   PriceSourceCoinGecko,
		}
		_ = s.priceRepo.RecordPrice(ctx, pricePoint)
	}

	s.circuitBreaker.RecordSuccess()
	return price, nil
}

// GetDecimals returns the decimals for an asset by CoinGecko ID
// Useful for transaction handlers to convert human-readable amounts
func (s *Service) GetDecimals(ctx context.Context, coinGeckoID string) (int, error) {
	asset, err := s.repo.GetByCoinGeckoID(ctx, coinGeckoID)
	if err != nil {
		return 8, nil // Default to 8 decimals
	}
	return asset.Decimals, nil
}

// ---- Symbol Resolution ----

// ResolveSymbol resolves a symbol to an asset UUID
// Returns ErrAmbiguousSymbol if symbol exists on multiple chains and chainID is nil
func (s *Service) ResolveSymbol(ctx context.Context, symbol string, chainID *string) (uuid.UUID, error) {
	asset, err := s.repo.GetBySymbol(ctx, symbol, chainID)
	if err != nil {
		return uuid.Nil, err
	}
	return asset.ID, nil
}

// SearchAssetsWithFallback searches local registry first, then falls back to provider
// Any new assets found from provider are saved to the registry
func (s *Service) SearchAssetsWithFallback(ctx context.Context, query string) ([]Asset, error) {
	// First, search local registry
	localAssets, err := s.repo.Search(ctx, query, 20)
	if err != nil {
		return nil, fmt.Errorf("failed to search local registry: %w", err)
	}

	// If we found results locally, return them
	if len(localAssets) > 0 {
		return localAssets, nil
	}

	// Fall back to provider search
	if s.priceProvider == nil {
		return []Asset{}, nil
	}

	coins, err := s.priceProvider.SearchCoins(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search provider: %w", err)
	}

	// Convert provider results to assets and save to registry
	assets := make([]Asset, 0, len(coins))
	for _, coin := range coins {
		// Check if already exists by CoinGecko ID
		existing, err := s.repo.GetByCoinGeckoID(ctx, coin.ID)
		if err == nil && existing != nil {
			assets = append(assets, *existing)
			continue
		}

		// Create new asset from provider data
		asset := NewAsset(coin.Symbol, coin.Name, coin.ID, 8) // default to 8 decimals
		if coin.MarketCapRank > 0 {
			asset.WithMarketCapRank(coin.MarketCapRank)
		}

		// Save to registry
		if err := s.repo.Create(ctx, asset); err != nil {
			// Ignore duplicate errors (race condition)
			if err != ErrDuplicateAsset {
				continue
			}
		}

		assets = append(assets, *asset)
	}

	return assets, nil
}
