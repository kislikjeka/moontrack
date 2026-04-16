// apps/backend/internal/platform/price/provider_coingecko.go
package price

import (
	"context"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// CoinGeckoBridge adapts the existing coingecko-capable asset.Service into a Provider.
// It exposes only the subset of methods the provider needs, so tests can stub it easily.
type CoinGeckoBridge interface {
	GetCurrentPriceByCoinGeckoID(ctx context.Context, coinGeckoID string) (*big.Int, error)
	GetHistoricalPriceByCoinGeckoID(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error)
}

// CoinGeckoProvider wraps the existing CoinGecko-backed asset.Service as a price.Provider.
type CoinGeckoProvider struct {
	b CoinGeckoBridge
}

func NewCoinGeckoProvider(b CoinGeckoBridge) *CoinGeckoProvider {
	return &CoinGeckoProvider{b: b}
}

func (p *CoinGeckoProvider) Name() Source { return SourceCoinGecko }

// GetPrice returns the current USD price for an asset via CoinGecko.
// Returns ErrNotFound if the asset has no CoinGeckoID or the provider returns nil.
// Returns ErrTransient on any API error.
func (p *CoinGeckoProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
	if a.CoinGeckoID == "" {
		return nil, ErrNotFound
	}
	priceBI, err := p.b.GetCurrentPriceByCoinGeckoID(ctx, a.CoinGeckoID)
	if err != nil {
		return nil, ErrTransient
	}
	if priceBI == nil {
		return nil, ErrNotFound
	}
	return priceBI, nil
}

// GetHistoricalPrice returns a day-granular historical price via CoinGecko.
// The timestamp is normalized to UTC midnight since the free tier is day-granular.
// Returns ErrNotFound if no CoinGeckoID or provider returns nil.
// Returns ErrTransient on any API error.
func (p *CoinGeckoProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, error) {
	if a.CoinGeckoID == "" {
		return nil, ErrNotFound
	}
	// Normalize to UTC midnight — CoinGecko free tier is day-granular.
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	priceBI, err := p.b.GetHistoricalPriceByCoinGeckoID(ctx, a.CoinGeckoID, day)
	if err != nil {
		return nil, ErrTransient
	}
	if priceBI == nil {
		return nil, ErrNotFound
	}
	return &HistoricalPrice{PriceUSD: priceBI, Timestamp: day, Confidence: 1}, nil
}
