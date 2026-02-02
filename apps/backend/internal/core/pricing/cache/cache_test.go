package cache_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/core/pricing/cache"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestCache creates a test Redis cache
func setupTestCache(t *testing.T) *cache.Cache {
	// Use a test Redis database (DB 15 for tests)
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15, // Use separate DB for tests
	})

	// Verify Redis is available
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Skipping test: Redis not available")
	}

	// Clear test database
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("Failed to flush test database: %v", err)
	}

	return cache.NewCache(client)
}

func TestCache_SetAndGet(t *testing.T) {
	c := setupTestCache(t)
	ctx := context.Background()

	t.Run("Set_and_Get_Price", func(t *testing.T) {
		assetID := "bitcoin"
		price := big.NewInt(4567890000000) // $45,678.90 * 10^8
		source := "coingecko"

		// Set price
		err := c.Set(ctx, assetID, price, source)
		require.NoError(t, err)

		// Get price
		retrievedPrice, found, err := c.Get(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, price.String(), retrievedPrice.String())
	})

	t.Run("Get_NonExistent_Price", func(t *testing.T) {
		price, found, err := c.Get(ctx, "nonexistent-asset")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, price)
	})

	t.Run("Set_With_Large_Number", func(t *testing.T) {
		assetID := "large-asset"
		// Test with max uint256 value
		price := new(big.Int)
		price.SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
		source := "manual"

		err := c.Set(ctx, assetID, price, source)
		require.NoError(t, err)

		retrievedPrice, found, err := c.Get(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, price.String(), retrievedPrice.String())
	})
}

func TestCache_TTL(t *testing.T) {
	c := setupTestCache(t)
	ctx := context.Background()

	t.Run("Price_Expires_After_TTL", func(t *testing.T) {
		// Create cache with short TTL
		client := redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
			DB:   15,
		})
		shortTTLCache := cache.NewCacheWithTTL(client, 1*time.Second)

		assetID := "bitcoin"
		price := big.NewInt(4567890000000)
		source := "coingecko"

		// Set price
		err := shortTTLCache.Set(ctx, assetID, price, source)
		require.NoError(t, err)

		// Get immediately - should exist
		_, found, err := shortTTLCache.Get(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)

		// Wait for TTL to expire
		time.Sleep(1500 * time.Millisecond)

		// Get after expiry - should not exist
		_, found, err = shortTTLCache.Get(ctx, assetID)
		require.NoError(t, err)
		assert.False(t, found, "Price should have expired")
	})

	t.Run("Custom_TTL", func(t *testing.T) {
		assetID := "ethereum"
		price := big.NewInt(300000000000) // $3,000 * 10^8
		source := "coingecko"
		customTTL := 2 * time.Second

		err := c.SetWithTTL(ctx, assetID, price, source, customTTL)
		require.NoError(t, err)

		// Should exist immediately
		_, found, err := c.Get(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)

		// Should still exist after 1 second
		time.Sleep(1 * time.Second)
		_, found, err = c.Get(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)

		// Should expire after 2+ seconds
		time.Sleep(1500 * time.Millisecond)
		_, found, err = c.Get(ctx, assetID)
		require.NoError(t, err)
		assert.False(t, found)
	})
}

func TestCache_StaleCache(t *testing.T) {
	c := setupTestCache(t)
	ctx := context.Background()

	t.Run("Set_and_Get_Stale_Price", func(t *testing.T) {
		assetID := "bitcoin"
		price := big.NewInt(4567890000000)
		source := "coingecko"

		// Set stale price
		err := c.SetStale(ctx, assetID, price, source)
		require.NoError(t, err)

		// Get stale price
		retrievedPrice, found, err := c.GetStale(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, price.String(), retrievedPrice.String())
	})

	t.Run("Stale_Cache_Independent_From_Regular", func(t *testing.T) {
		assetID := "ethereum"
		regularPrice := big.NewInt(300000000000)
		stalePrice := big.NewInt(290000000000)

		// Set both regular and stale prices
		err := c.Set(ctx, assetID, regularPrice, "coingecko")
		require.NoError(t, err)
		err = c.SetStale(ctx, assetID, stalePrice, "coingecko")
		require.NoError(t, err)

		// Get regular price
		price, found, err := c.Get(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, regularPrice.String(), price.String())

		// Get stale price
		stale, found, err := c.GetStale(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, stalePrice.String(), stale.String())
	})
}

func TestCache_GetMultiple(t *testing.T) {
	c := setupTestCache(t)
	ctx := context.Background()

	t.Run("Get_Multiple_Prices", func(t *testing.T) {
		// Set multiple prices
		assets := map[string]*big.Int{
			"bitcoin":  big.NewInt(4567890000000),
			"ethereum": big.NewInt(300000000000),
			"usd-coin": big.NewInt(100000000),
		}

		for assetID, price := range assets {
			err := c.Set(ctx, assetID, price, "coingecko")
			require.NoError(t, err)
		}

		// Get all prices
		assetIDs := []string{"bitcoin", "ethereum", "usd-coin"}
		prices, err := c.GetMultiple(ctx, assetIDs)
		require.NoError(t, err)
		assert.Len(t, prices, 3)

		// Verify each price
		for assetID, expectedPrice := range assets {
			retrievedPrice, found := prices[assetID]
			assert.True(t, found, "Price for %s should be found", assetID)
			assert.Equal(t, expectedPrice.String(), retrievedPrice.String())
		}
	})

	t.Run("Get_Multiple_With_Missing", func(t *testing.T) {
		// Set only one price
		err := c.Set(ctx, "bitcoin", big.NewInt(4567890000000), "coingecko")
		require.NoError(t, err)

		// Try to get multiple, including missing
		assetIDs := []string{"bitcoin", "missing-asset"}
		prices, err := c.GetMultiple(ctx, assetIDs)
		require.NoError(t, err)

		// Should return only the found price
		assert.Len(t, prices, 1)
		_, found := prices["bitcoin"]
		assert.True(t, found)
		_, found = prices["missing-asset"]
		assert.False(t, found)
	})

	t.Run("Get_Multiple_Empty_List", func(t *testing.T) {
		prices, err := c.GetMultiple(ctx, []string{})
		require.NoError(t, err)
		assert.Empty(t, prices)
	})
}

func TestCache_Delete(t *testing.T) {
	c := setupTestCache(t)
	ctx := context.Background()

	assetID := "bitcoin"
	price := big.NewInt(4567890000000)

	// Set price
	err := c.Set(ctx, assetID, price, "coingecko")
	require.NoError(t, err)

	// Verify it exists
	_, found, err := c.Get(ctx, assetID)
	require.NoError(t, err)
	assert.True(t, found)

	// Delete price
	err = c.Delete(ctx, assetID)
	require.NoError(t, err)

	// Verify it's gone
	_, found, err = c.Get(ctx, assetID)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestCache_Clear(t *testing.T) {
	c := setupTestCache(t)
	ctx := context.Background()

	// Set multiple prices
	assets := []string{"bitcoin", "ethereum", "usd-coin"}
	for _, assetID := range assets {
		err := c.Set(ctx, assetID, big.NewInt(100000000), "coingecko")
		require.NoError(t, err)
	}

	// Verify all exist
	for _, assetID := range assets {
		_, found, err := c.Get(ctx, assetID)
		require.NoError(t, err)
		assert.True(t, found)
	}

	// Clear all prices
	err := c.Clear(ctx)
	require.NoError(t, err)

	// Verify all are gone
	for _, assetID := range assets {
		_, found, err := c.Get(ctx, assetID)
		require.NoError(t, err)
		assert.False(t, found, "Price for %s should be cleared", assetID)
	}
}
