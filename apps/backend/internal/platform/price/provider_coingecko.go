// apps/backend/internal/platform/price/provider_coingecko.go
package price

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/coingecko"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// CoinGeckoBridge adapts the existing coingecko-capable asset.Service into a Provider.
// It exposes only the subset of methods the provider needs, so tests can stub it easily.
//
// The *NoFallback variants bypass the stale-cache fallback so callers that need
// the raw provider signal (rate-limit / transient) can receive it. The backfill
// worker requires this so it can reschedule without burning attempts.
type CoinGeckoBridge interface {
	GetCurrentPriceByCoinGeckoIDNoFallback(ctx context.Context, coinGeckoID string) (*big.Int, error)
	GetHistoricalPriceByCoinGeckoIDNoFallback(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error)
}

// CoinGeckoProvider wraps the existing CoinGecko-backed asset.Service as a price.Provider.
type CoinGeckoProvider struct {
	b CoinGeckoBridge
}

func NewCoinGeckoProvider(b CoinGeckoBridge) *CoinGeckoProvider {
	return &CoinGeckoProvider{b: b}
}

func (p *CoinGeckoProvider) Name() Source { return SourceCoinGecko }

// classifyErr translates a bridge error into a price-layer error. Rate-limit
// errors from the underlying CoinGecko client are surfaced as *RateLimitedError
// so the worker can honor Retry-After instead of treating it as a transient
// black box.
func classifyErr(err error) error {
	if err == nil {
		return nil
	}
	var rle *coingecko.RateLimitError
	if errors.As(err, &rle) {
		return &RateLimitedError{RetryAfter: rle.RetryAfter}
	}
	return ErrTransient
}

// GetPrice returns the current USD price for an asset via CoinGecko.
// Returns ErrNotFound if the asset has no CoinGeckoID or the provider returns nil.
// Returns *RateLimitedError on 429 and ErrTransient on any other API error.
func (p *CoinGeckoProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
	if a.CoinGeckoID == "" {
		return nil, ErrNotFound
	}
	priceBI, err := p.b.GetCurrentPriceByCoinGeckoIDNoFallback(ctx, a.CoinGeckoID)
	if err != nil {
		return nil, classifyErr(err)
	}
	if priceBI == nil {
		return nil, ErrNotFound
	}
	return priceBI, nil
}

// GetHistoricalPrice returns a day-granular historical price via CoinGecko.
// The timestamp is normalized to UTC midnight since the free tier is day-granular.
// Returns ErrNotFound if no CoinGeckoID or provider returns nil.
// Returns *RateLimitedError on 429 and ErrTransient on any other API error.
func (p *CoinGeckoProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, error) {
	if a.CoinGeckoID == "" {
		return nil, ErrNotFound
	}
	// Normalize to UTC midnight — CoinGecko free tier is day-granular.
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	priceBI, err := p.b.GetHistoricalPriceByCoinGeckoIDNoFallback(ctx, a.CoinGeckoID, day)
	if err != nil {
		return nil, classifyErr(err)
	}
	if priceBI == nil {
		return nil, ErrNotFound
	}
	return &HistoricalPrice{PriceUSD: priceBI, Timestamp: day, Confidence: 1}, nil
}
