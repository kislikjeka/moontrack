package coingecko

import (
	"context"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// PriceProviderAdapter adapts coingecko.Client to the asset.PriceProvider interface
type PriceProviderAdapter struct {
	client *Client
}

// NewPriceProviderAdapter creates a new adapter
func NewPriceProviderAdapter(client *Client) *PriceProviderAdapter {
	return &PriceProviderAdapter{client: client}
}

// GetCurrentPrices fetches current prices for multiple assets
func (a *PriceProviderAdapter) GetCurrentPrices(ctx context.Context, coinGeckoIDs []string) (map[string]*big.Int, error) {
	return a.client.GetCurrentPrices(ctx, coinGeckoIDs)
}

// GetHistoricalPrice fetches the price at a specific date
func (a *PriceProviderAdapter) GetHistoricalPrice(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error) {
	return a.client.GetHistoricalPrice(ctx, coinGeckoID, date)
}

// SearchCoins searches for coins matching a query
func (a *PriceProviderAdapter) SearchCoins(ctx context.Context, query string) ([]asset.CoinSearchResult, error) {
	coins, err := a.client.SearchCoins(ctx, query)
	if err != nil {
		return nil, err
	}

	results := make([]asset.CoinSearchResult, len(coins))
	for i, coin := range coins {
		results[i] = asset.CoinSearchResult{
			ID:            coin.ID,
			Symbol:        coin.Symbol,
			Name:          coin.Name,
			MarketCapRank: coin.MarketCapRank,
		}
	}

	return results, nil
}

// Ensure PriceProviderAdapter implements asset.PriceProvider
var _ asset.PriceProvider = (*PriceProviderAdapter)(nil)
