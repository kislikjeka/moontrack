package service

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/kislikjeka/moontrack/internal/core/pricing/cache"
	"github.com/kislikjeka/moontrack/internal/core/pricing/coingecko"
	"github.com/kislikjeka/moontrack/internal/core/pricing/domain"
)

// PriceRepository defines the interface for price persistence
type PriceRepository interface {
	Save(ctx context.Context, snapshot *domain.PriceSnapshot) error
	Get(ctx context.Context, assetID string, date time.Time) (*domain.PriceSnapshot, error)
	GetLatest(ctx context.Context, assetID string) (*domain.PriceSnapshot, error)
}

// PriceService orchestrates price fetching with caching and fallback
type PriceService struct {
	coinGeckoClient *coingecko.Client
	cache           *cache.Cache
	repository      PriceRepository
	circuitBreaker  *CircuitBreaker
	mu              sync.RWMutex
}

// NewPriceService creates a new price service
func NewPriceService(
	coinGeckoClient *coingecko.Client,
	cache *cache.Cache,
	repository PriceRepository,
) *PriceService {
	return &PriceService{
		coinGeckoClient: coinGeckoClient,
		cache:           cache,
		repository:      repository,
		circuitBreaker:  NewCircuitBreaker(3, 5*time.Minute), // 3 failures, 5-minute cooldown
	}
}

// GetCurrentPrice fetches the current USD price for an asset with multi-layer fallback
// Fallback order: Cache (60s) → Database → CoinGecko API → Stale Cache (24h) → Error
func (s *PriceService) GetCurrentPrice(ctx context.Context, assetID string) (*big.Int, error) {
	// Layer 1: Check cache (60s TTL)
	price, found, err := s.cache.Get(ctx, assetID)
	if err == nil && found {
		return price, nil
	}

	// Layer 2: Check database for recent price (within last 5 minutes)
	if s.repository != nil {
		snapshot, err := s.repository.GetLatest(ctx, assetID)
		if err == nil && snapshot != nil {
			// Check if price is recent (within 5 minutes)
			if time.Since(snapshot.CreatedAt) < 5*time.Minute {
				// Cache it for 60 seconds
				_ = s.cache.Set(ctx, assetID, snapshot.USDPrice, string(snapshot.Source))
				return snapshot.USDPrice, nil
			}
		}
	}

	// Layer 3: Fetch from CoinGecko API (if circuit breaker allows)
	if s.circuitBreaker.CanAttempt() {
		price, err := s.fetchFromCoinGecko(ctx, assetID)
		if err == nil {
			// Success: cache it and save to database
			_ = s.cache.Set(ctx, assetID, price, "coingecko")
			_ = s.cache.SetStale(ctx, assetID, price, "coingecko")

			if s.repository != nil {
				snapshot := &domain.PriceSnapshot{
					AssetID:      assetID,
					USDPrice:     price,
					Source:       domain.PriceSourceCoinGecko,
					SnapshotDate: time.Now().UTC(),
					CreatedAt:    time.Now().UTC(),
				}
				_ = s.repository.Save(ctx, snapshot)
			}

			s.circuitBreaker.RecordSuccess()
			return price, nil
		}

		// API failure: record it
		s.circuitBreaker.RecordFailure()
	}

	// Layer 4: Fallback to stale cache (24h TTL)
	price, found, err = s.cache.GetStale(ctx, assetID)
	if err == nil && found {
		return price, &StalePriceWarning{
			Message: fmt.Sprintf("Using stale cached price for %s (API unavailable)", assetID),
			Price:   price,
		}
	}

	// Layer 5: No price available
	return nil, domain.ErrPriceNotFound
}

// GetHistoricalPrice fetches the USD price for an asset on a specific date
// Fallback order: Database → CoinGecko API → Error
func (s *PriceService) GetHistoricalPrice(ctx context.Context, assetID string, date time.Time) (*big.Int, error) {
	// Normalize date to midnight UTC
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	// Layer 1: Check database
	if s.repository != nil {
		snapshot, err := s.repository.Get(ctx, assetID, date)
		if err == nil && snapshot != nil {
			return snapshot.USDPrice, nil
		}
	}

	// Layer 2: Fetch from CoinGecko API (if circuit breaker allows)
	if !s.circuitBreaker.CanAttempt() {
		return nil, domain.ErrPriceAPIUnavailable
	}

	price, err := s.coinGeckoClient.GetHistoricalPrice(ctx, assetID, date)
	if err != nil {
		s.circuitBreaker.RecordFailure()
		return nil, fmt.Errorf("failed to fetch historical price: %w", err)
	}

	// Success: save to database for future queries
	if s.repository != nil {
		snapshot := &domain.PriceSnapshot{
			AssetID:      assetID,
			USDPrice:     price,
			Source:       domain.PriceSourceCoinGecko,
			SnapshotDate: date,
			CreatedAt:    time.Now().UTC(),
		}
		_ = s.repository.Save(ctx, snapshot)
	}

	s.circuitBreaker.RecordSuccess()
	return price, nil
}

// GetMultiplePrices fetches current USD prices for multiple assets in parallel
func (s *PriceService) GetMultiplePrices(ctx context.Context, assetIDs []string) (map[string]*big.Int, error) {
	if len(assetIDs) == 0 {
		return make(map[string]*big.Int), nil
	}

	// Try to get all from cache first
	cached, err := s.cache.GetMultiple(ctx, assetIDs)
	if err == nil && len(cached) == len(assetIDs) {
		// All prices found in cache
		return cached, nil
	}

	// Find missing asset IDs
	missing := make([]string, 0)
	for _, assetID := range assetIDs {
		if _, found := cached[assetID]; !found {
			missing = append(missing, assetID)
		}
	}

	// Fetch missing prices from CoinGecko (if circuit breaker allows)
	if len(missing) > 0 && s.circuitBreaker.CanAttempt() {
		prices, err := s.coinGeckoClient.GetCurrentPrices(ctx, missing)
		if err == nil {
			// Cache and save all fetched prices
			for assetID, price := range prices {
				cached[assetID] = price
				_ = s.cache.Set(ctx, assetID, price, "coingecko")
				_ = s.cache.SetStale(ctx, assetID, price, "coingecko")

				if s.repository != nil {
					snapshot := &domain.PriceSnapshot{
						AssetID:      assetID,
						USDPrice:     price,
						Source:       domain.PriceSourceCoinGecko,
						SnapshotDate: time.Now().UTC(),
						CreatedAt:    time.Now().UTC(),
					}
					_ = s.repository.Save(ctx, snapshot)
				}
			}
			s.circuitBreaker.RecordSuccess()
		} else {
			s.circuitBreaker.RecordFailure()
		}
	}

	return cached, nil
}

// fetchFromCoinGecko fetches a single price from CoinGecko API
func (s *PriceService) fetchFromCoinGecko(ctx context.Context, assetID string) (*big.Int, error) {
	prices, err := s.coinGeckoClient.GetCurrentPrices(ctx, []string{assetID})
	if err != nil {
		return nil, err
	}

	price, found := prices[assetID]
	if !found {
		return nil, fmt.Errorf("price not found for asset %s", assetID)
	}

	return price, nil
}

// IsCircuitOpen returns true if the circuit breaker is open
func (s *PriceService) IsCircuitOpen() bool {
	return !s.circuitBreaker.CanAttempt()
}

// StalePriceWarning represents a warning when using stale cached prices
type StalePriceWarning struct {
	Message string
	Price   *big.Int
}

func (w *StalePriceWarning) Error() string {
	return w.Message
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
	CircuitClosed CircuitState = iota // Normal operation
	CircuitOpen                        // Too many failures, blocking requests
	CircuitHalfOpen                    // Testing if service recovered
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

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// RefreshPrices fetches and caches current prices for the given asset IDs
// This is meant to be called periodically to keep cache warm
func (s *PriceService) RefreshPrices(ctx context.Context, assetIDs []string) error {
	if len(assetIDs) == 0 {
		return nil
	}

	_, err := s.GetMultiplePrices(ctx, assetIDs)
	return err
}

// DefaultAssets returns the default list of assets to track
func DefaultAssets() []string {
	return []string{
		"BTC",
		"ETH",
		"USDC",
		"USDT",
		"SOL",
		"BNB",
		"ADA",
		"DOT",
		"MATIC",
		"AVAX",
	}
}
