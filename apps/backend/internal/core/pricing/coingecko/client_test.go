package coingecko_test

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/core/pricing/coingecko"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCoinGeckoIntegration tests the CoinGecko API client with real API key
// This test requires COINGECKO_API_KEY environment variable
func TestCoinGeckoIntegration(t *testing.T) {
	apiKey := os.Getenv("COINGECKO_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: COINGECKO_API_KEY not set")
	}

	client := coingecko.NewClient(apiKey)
	ctx := context.Background()

	t.Run("GetCurrentPrices_Success", func(t *testing.T) {
		// Test fetching prices for Bitcoin and Ethereum
		assetIDs := []string{"bitcoin", "ethereum"}
		prices, err := client.GetCurrentPrices(ctx, assetIDs)

		require.NoError(t, err)
		assert.Len(t, prices, 2)

		// Verify Bitcoin price
		btcPrice, found := prices["bitcoin"]
		assert.True(t, found, "Bitcoin price should be returned")
		assert.NotNil(t, btcPrice)
		assert.True(t, btcPrice.Cmp(big.NewInt(0)) > 0, "Bitcoin price should be positive")

		// Verify Ethereum price
		ethPrice, found := prices["ethereum"]
		assert.True(t, found, "Ethereum price should be returned")
		assert.NotNil(t, ethPrice)
		assert.True(t, ethPrice.Cmp(big.NewInt(0)) > 0, "Ethereum price should be positive")

		// Verify prices are scaled by 10^8
		// BTC should be > $10,000 * 10^8 = 1,000,000,000,000
		minBTCPrice := new(big.Int).Mul(big.NewInt(10000), big.NewInt(100000000))
		assert.True(t, btcPrice.Cmp(minBTCPrice) > 0, "Bitcoin price should be > $10,000")
	})

	t.Run("GetCurrentPrices_EmptyList", func(t *testing.T) {
		prices, err := client.GetCurrentPrices(ctx, []string{})
		require.NoError(t, err)
		assert.Empty(t, prices)
	})

	t.Run("GetCurrentPrices_InvalidAsset", func(t *testing.T) {
		// CoinGecko returns empty map for invalid asset IDs (not an error)
		assetIDs := []string{"invalid-asset-id-12345"}
		prices, err := client.GetCurrentPrices(ctx, assetIDs)

		require.NoError(t, err)
		assert.Empty(t, prices, "Invalid asset should return empty map")
	})

	t.Run("GetHistoricalPrice_Success", func(t *testing.T) {
		// Test fetching historical price for Bitcoin on a specific date
		// Use a date in the past (1 week ago)
		date := time.Now().AddDate(0, 0, -7).UTC()

		price, err := client.GetHistoricalPrice(ctx, "bitcoin", date)

		require.NoError(t, err)
		assert.NotNil(t, price)
		assert.True(t, price.Cmp(big.NewInt(0)) > 0, "Historical Bitcoin price should be positive")

		// Verify price is scaled by 10^8
		minBTCPrice := new(big.Int).Mul(big.NewInt(10000), big.NewInt(100000000))
		assert.True(t, price.Cmp(minBTCPrice) > 0, "Historical Bitcoin price should be > $10,000")
	})

	t.Run("GetHistoricalPrice_InvalidAsset", func(t *testing.T) {
		date := time.Now().AddDate(0, 0, -7).UTC()

		price, err := client.GetHistoricalPrice(ctx, "invalid-asset-id-12345", date)

		assert.Error(t, err)
		assert.Nil(t, price)
	})

	t.Run("RateLimitHandling", func(t *testing.T) {
		// This test is informational - it won't trigger rate limiting in normal use
		// but demonstrates that the client handles rate limit errors properly

		// Make multiple rapid requests to approach rate limit
		// (30 requests/minute limit on Demo plan)
		ctx := context.Background()
		assetIDs := []string{"bitcoin"}

		successCount := 0
		rateLimitCount := 0

		for i := 0; i < 5; i++ {
			_, err := client.GetCurrentPrices(ctx, assetIDs)
			if err != nil {
				if coingecko.IsRateLimitError(err) {
					rateLimitCount++
					t.Logf("Rate limit hit on request %d: %v", i+1, err)
				}
			} else {
				successCount++
			}

			// Small delay to avoid overwhelming the API
			time.Sleep(100 * time.Millisecond)
		}

		t.Logf("Completed %d successful requests, %d rate limited", successCount, rateLimitCount)
		// We expect most requests to succeed with small batch
		assert.True(t, successCount > 0, "Should have at least one successful request")
	})
}

// TestScalingPrecision verifies that price scaling maintains precision
func TestScalingPrecision(t *testing.T) {
	apiKey := os.Getenv("COINGECKO_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping test: COINGECKO_API_KEY not set")
	}

	client := coingecko.NewClient(apiKey)
	ctx := context.Background()

	t.Run("VerifyDecimalPrecision", func(t *testing.T) {
		// Fetch a stablecoin price (should be close to $1.00)
		prices, err := client.GetCurrentPrices(ctx, []string{"usd-coin"})
		require.NoError(t, err)

		usdcPrice, found := prices["usd-coin"]
		assert.True(t, found)
		assert.NotNil(t, usdcPrice)

		// USDC should be approximately 1.00 USD ± 0.05
		// Scaled: 100000000 ± 5000000
		oneDollar := big.NewInt(100000000) // 1.00 * 10^8
		tolerance := big.NewInt(5000000)   // 0.05 * 10^8

		diff := new(big.Int).Sub(usdcPrice, oneDollar)
		diff.Abs(diff)

		assert.True(t, diff.Cmp(tolerance) <= 0,
			"USDC price should be within ±$0.05 of $1.00, got: %s", usdcPrice.String())
	})
}
