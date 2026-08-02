// apps/backend/internal/platform/price/provider_coingecko.go
package price

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/coingecko"
)

// CoinGeckoBridge is the CoinGecko-addressable price source this provider needs.
// It is the subset of the gateway client the provider uses, so tests can stub it.
//
// The methods must NOT apply a stale-cache fallback. The backfill worker
// distinguishes "the provider says there is no data" from "the provider could not
// be reached", and spends one of a job's finite attempts only on the former; a
// fallback that answered with a stale price, or that masked a 429 as a miss,
// would collapse that distinction and burn attempts during an outage.
//
// This used to be satisfied by asset.Service's *NoFallback methods, which existed
// solely to opt out of the caching that same service added. With asset.Service
// gone (#59) the gateway client satisfies it directly — it never had a fallback
// to suppress, so the plain methods carry the required semantics.
type CoinGeckoBridge interface {
	GetCurrentPrices(ctx context.Context, coinGeckoIDs []string) (map[string]*big.Int, error)
	GetHistoricalPrice(ctx context.Context, coinGeckoID string, date time.Time) (*big.Int, error)
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
func (p *CoinGeckoProvider) GetPrice(ctx context.Context, a Asset) (*big.Int, error) {
	if a.CoinGeckoID == "" {
		return nil, ErrNotFound
	}
	prices, err := p.b.GetCurrentPrices(ctx, []string{a.CoinGeckoID})
	if err != nil {
		return nil, classifyErr(err)
	}
	// A slug absent from the response is CoinGecko positively reporting no data
	// for it, which is ErrNotFound rather than an error condition.
	priceBI, ok := prices[a.CoinGeckoID]
	if !ok || priceBI == nil {
		return nil, ErrNotFound
	}
	return priceBI, nil
}

// GetHistoricalPrice returns a day-granular historical price via CoinGecko.
// The timestamp is normalized to UTC midnight since the free tier is day-granular.
// Returns ErrNotFound if no CoinGeckoID or provider returns nil.
// Returns *RateLimitedError on 429 and ErrTransient on any other API error.
func (p *CoinGeckoProvider) GetHistoricalPrice(ctx context.Context, a Asset, at time.Time) (*HistoricalPrice, error) {
	if a.CoinGeckoID == "" {
		return nil, ErrNotFound
	}
	// Normalize to UTC midnight — CoinGecko free tier is day-granular.
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	priceBI, err := p.b.GetHistoricalPrice(ctx, a.CoinGeckoID, day)
	if err != nil {
		return nil, classifyErr(err)
	}
	if priceBI == nil {
		return nil, ErrNotFound
	}
	return &HistoricalPrice{PriceUSD: priceBI, Timestamp: day, Confidence: 1}, nil
}
