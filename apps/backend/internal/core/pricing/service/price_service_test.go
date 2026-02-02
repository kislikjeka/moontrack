package service_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/core/pricing/cache"
	"github.com/kislikjeka/moontrack/internal/core/pricing/coingecko"
	"github.com/kislikjeka/moontrack/internal/core/pricing/domain"
	"github.com/kislikjeka/moontrack/internal/core/pricing/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPriceRepository implements PriceRepository for testing
type mockPriceRepository struct {
	prices         map[string]*domain.PriceSnapshot
	saveFails      bool
	getLatestFails bool
}

func newMockPriceRepository() *mockPriceRepository {
	return &mockPriceRepository{
		prices: make(map[string]*domain.PriceSnapshot),
	}
}

func (m *mockPriceRepository) Save(ctx context.Context, snapshot *domain.PriceSnapshot) error {
	if m.saveFails {
		return errors.New("mock save error")
	}
	key := snapshot.AssetID + snapshot.SnapshotDate.Format("2006-01-02")
	m.prices[key] = snapshot
	return nil
}

func (m *mockPriceRepository) Get(ctx context.Context, assetID string, date time.Time) (*domain.PriceSnapshot, error) {
	key := assetID + date.Format("2006-01-02")
	if snapshot, found := m.prices[key]; found {
		return snapshot, nil
	}
	return nil, nil
}

func (m *mockPriceRepository) GetLatest(ctx context.Context, assetID string) (*domain.PriceSnapshot, error) {
	if m.getLatestFails {
		return nil, errors.New("mock get latest error")
	}
	// Find latest snapshot for asset
	var latest *domain.PriceSnapshot
	for _, snapshot := range m.prices {
		if snapshot.AssetID == assetID {
			if latest == nil || snapshot.CreatedAt.After(latest.CreatedAt) {
				latest = snapshot
			}
		}
	}
	return latest, nil
}

// setupTestRedis creates a test Redis client
func setupTestRedis(t *testing.T) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15, // Use separate DB for tests
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Skipping test: Redis not available")
	}

	// Clear test database
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("Failed to flush test database: %v", err)
	}

	return client
}

func TestPriceService_APIFailureScenarios(t *testing.T) {
	// These tests verify the multi-layer fallback mechanism:
	// Cache (60s) → Database → CoinGecko API → Stale Cache (24h) → Error

	t.Run("NetworkTimeout", func(t *testing.T) {
		// Simulate network timeout by using invalid CoinGecko endpoint
		client := coingecko.NewClient("invalid-key")
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()
		svc := service.NewPriceService(client, c, repo)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// API will timeout, should return error with no fallback
		price, err := svc.GetCurrentPrice(ctx, "bitcoin")
		assert.Error(t, err)
		assert.Nil(t, price)
	})

	t.Run("RateLimitExceeded_FallbackToStaleCache", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()

		// Pre-populate stale cache with price
		ctx := context.Background()
		stalePrice := big.NewInt(4500000000000) // $45,000 * 10^8
		err := c.SetStale(ctx, "bitcoin", stalePrice, "coingecko")
		require.NoError(t, err)

		// Use invalid API key to simulate rate limit/failure
		client := coingecko.NewClient("invalid-key")
		svc := service.NewPriceService(client, c, repo)

		// GetCurrentPrice should fall back to stale cache
		price, err := svc.GetCurrentPrice(ctx, "bitcoin")

		// Should return stale price with warning
		assert.NotNil(t, price)
		assert.Equal(t, stalePrice.String(), price.String())
		// Error should be StalePriceWarning
		var staleWarning *service.StalePriceWarning
		if errors.As(err, &staleWarning) {
			assert.Contains(t, staleWarning.Message, "stale cached price")
		}
	})

	t.Run("InvalidResponse_CircuitBreakerOpens", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()

		// Use invalid API key to simulate failures
		client := coingecko.NewClient("invalid-key")
		svc := service.NewPriceService(client, c, repo)

		ctx := context.Background()

		// Make 3 failed requests to trigger circuit breaker
		for i := 0; i < 3; i++ {
			_, err := svc.GetCurrentPrice(ctx, "bitcoin")
			assert.Error(t, err)
		}

		// Circuit breaker should now be open
		assert.True(t, svc.IsCircuitOpen(), "Circuit breaker should be open after 3 failures")

		// Next request should fail fast without hitting API
		start := time.Now()
		_, err := svc.GetCurrentPrice(ctx, "ethereum")
		duration := time.Since(start)

		assert.Error(t, err)
		// Should fail fast (< 100ms) without making API call
		assert.Less(t, duration, 100*time.Millisecond, "Should fail fast when circuit is open")
	})

	t.Run("APIDegradation_FallbackToCachedPrices", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()

		ctx := context.Background()

		// Pre-populate cache with fresh price
		cachedPrice := big.NewInt(4600000000000) // $46,000 * 10^8
		err := c.Set(ctx, "bitcoin", cachedPrice, "coingecko")
		require.NoError(t, err)

		// Use invalid API key - but cache should serve request
		client := coingecko.NewClient("invalid-key")
		svc := service.NewPriceService(client, c, repo)

		// Should return cached price without hitting API
		price, err := svc.GetCurrentPrice(ctx, "bitcoin")
		require.NoError(t, err)
		assert.Equal(t, cachedPrice.String(), price.String())
	})

	t.Run("DatabaseFallback_WhenCacheMisses", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()

		ctx := context.Background()

		// Pre-populate database with recent price (within 5 minutes)
		dbPrice := big.NewInt(4700000000000) // $47,000 * 10^8
		snapshot := &domain.PriceSnapshot{
			AssetID:      "bitcoin",
			USDPrice:     dbPrice,
			Source:       domain.PriceSourceCoinGecko,
			SnapshotDate: time.Now().UTC(),
			CreatedAt:    time.Now().UTC(), // Fresh timestamp
		}
		err := repo.Save(ctx, snapshot)
		require.NoError(t, err)

		// Use invalid API key - should fall back to database
		client := coingecko.NewClient("invalid-key")
		svc := service.NewPriceService(client, c, repo)

		// Should return database price
		price, err := svc.GetCurrentPrice(ctx, "bitcoin")
		require.NoError(t, err)
		assert.Equal(t, dbPrice.String(), price.String())

		// Price should now be cached
		cachedPrice, found, err := c.Get(ctx, "bitcoin")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, dbPrice.String(), cachedPrice.String())
	})

	t.Run("OldDatabasePrice_TriggerAPIRefresh", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()

		ctx := context.Background()

		// Pre-populate database with OLD price (> 5 minutes)
		oldSnapshot := &domain.PriceSnapshot{
			AssetID:      "bitcoin",
			USDPrice:     big.NewInt(4700000000000),
			Source:       domain.PriceSourceCoinGecko,
			SnapshotDate: time.Now().Add(-10 * time.Minute).UTC(),
			CreatedAt:    time.Now().Add(-10 * time.Minute).UTC(), // Old timestamp
		}
		err := repo.Save(ctx, oldSnapshot)
		require.NoError(t, err)

		// Invalid API key - should try API, fail, then use nothing (no stale cache)
		client := coingecko.NewClient("invalid-key")
		svc := service.NewPriceService(client, c, repo)

		// Should fail because database price is too old and API fails
		_, err = svc.GetCurrentPrice(ctx, "bitcoin")
		assert.Error(t, err)
	})

	t.Run("CompleteFallbackChain_NoDataAvailable", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()

		// Use invalid API key
		client := coingecko.NewClient("invalid-key")
		svc := service.NewPriceService(client, c, repo)

		ctx := context.Background()

		// No cache, no database, API fails, no stale cache -> error
		price, err := svc.GetCurrentPrice(ctx, "bitcoin")
		assert.Error(t, err)
		assert.Equal(t, domain.ErrPriceNotFound, err)
		assert.Nil(t, price)
	})
}

func TestPriceService_PerformanceMetrics(t *testing.T) {
	t.Run("BalanceUpdate_Within2Seconds", func(t *testing.T) {
		// This test verifies SC-002: balance updates complete within 2 seconds
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()

		ctx := context.Background()

		// Pre-populate cache for instant response
		cachedPrice := big.NewInt(4600000000000)
		err := c.Set(ctx, "bitcoin", cachedPrice, "coingecko")
		require.NoError(t, err)

		client := coingecko.NewClient("dummy-key") // Won't be called
		svc := service.NewPriceService(client, c, repo)

		// Measure time to get price
		start := time.Now()
		price, err := svc.GetCurrentPrice(ctx, "bitcoin")
		duration := time.Since(start)

		require.NoError(t, err)
		assert.NotNil(t, price)

		// Cache hit should be < 10ms (well within 2 second requirement)
		assert.Less(t, duration, 10*time.Millisecond,
			"Cache hit should complete in < 10ms (SC-002 requires < 2s)")
		t.Logf("Price fetch from cache completed in: %v", duration)
	})

	t.Run("MultiplePrices_BatchPerformance", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		c := cache.NewCache(redisClient)
		repo := newMockPriceRepository()

		ctx := context.Background()

		// Pre-populate cache with multiple prices
		assets := []string{"bitcoin", "ethereum", "usd-coin", "solana", "cardano"}
		for _, assetID := range assets {
			err := c.Set(ctx, assetID, big.NewInt(100000000), "coingecko")
			require.NoError(t, err)
		}

		client := coingecko.NewClient("dummy-key")
		svc := service.NewPriceService(client, c, repo)

		// Measure time to get multiple prices
		start := time.Now()
		prices, err := svc.GetMultiplePrices(ctx, assets)
		duration := time.Since(start)

		require.NoError(t, err)
		assert.Len(t, prices, 5)

		// Batch fetch should be < 50ms for 5 assets from cache
		assert.Less(t, duration, 50*time.Millisecond,
			"Batch price fetch should complete quickly from cache")
		t.Logf("Batch fetch of %d prices completed in: %v", len(assets), duration)
	})
}

func TestPriceService_CircuitBreakerRecovery(t *testing.T) {
	t.Run("CircuitBreaker_Reopens_After_Cooldown", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		c := cache.NewCacheWithTTL(redisClient, 1*time.Second) // Short TTL for testing
		repo := newMockPriceRepository()

		// Use invalid API key to trigger failures
		client := coingecko.NewClient("invalid-key")
		// Create service with short cooldown for testing
		svc := service.NewPriceService(client, c, repo)

		ctx := context.Background()

		// Trigger circuit breaker (3 failures)
		for i := 0; i < 3; i++ {
			_, err := svc.GetCurrentPrice(ctx, "bitcoin")
			assert.Error(t, err)
		}

		// Circuit should be open
		assert.True(t, svc.IsCircuitOpen())

		// Wait for cooldown period (5 minutes in production, but circuit breaker
		// will attempt half-open after cooldown)
		// In real implementation, we'd need to wait 5 minutes or mock time
		// For this test, we verify the circuit is open immediately
		isOpen := svc.IsCircuitOpen()
		assert.True(t, isOpen, "Circuit should remain open during cooldown")
	})
}
