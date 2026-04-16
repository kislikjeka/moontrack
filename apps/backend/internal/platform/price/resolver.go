// apps/backend/internal/platform/price/resolver.go
package price

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Resolver walks an ordered provider chain. The first non-error success wins.
type Resolver struct {
	providers []Provider
	cache     *Cache
	log       *logger.Logger
}

// NewResolver returns a Resolver. cache may be nil; providers order defines priority.
func NewResolver(providers []Provider, cache *Cache, log *logger.Logger) *Resolver {
	return &Resolver{providers: providers, cache: cache, log: log.WithField("component", "price_resolver")}
}

// ResolveHistorical walks the chain, trying each provider for a historical price.
//
// Error semantics:
//   - Returns first success.
//   - Falls through on ErrNotFound, ErrLowConfidence, ErrUnsupportedChain.
//   - Returns ErrRateLimited if ANY provider was rate-limited AND no success.
//   - Returns ErrTransient on network/5xx without any success.
//   - Returns ErrNotFound if all providers returned NotFound-class errors.
func (r *Resolver) ResolveHistorical(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, Source, error) {
	var sawRateLimited, sawTransient bool
	var lastNotFound error

	for _, p := range r.providers {
		if r.cache != nil && a.ID != (asset.Asset{}).ID {
			if hp, ok, _ := r.cache.GetHistorical(ctx, p.Name(), a.ID, at); ok {
				return hp, p.Name(), nil
			}
		}

		hp, err := p.GetHistoricalPrice(ctx, a, at)
		if err == nil {
			if r.cache != nil && a.ID != (asset.Asset{}).ID {
				_ = r.cache.PutHistorical(ctx, p.Name(), a.ID, at, hp)
			}
			return hp, p.Name(), nil
		}
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrLowConfidence), errors.Is(err, ErrUnsupportedChain):
			lastNotFound = err
		case errors.Is(err, ErrRateLimited):
			sawRateLimited = true
		case errors.Is(err, ErrTransient):
			sawTransient = true
		default:
			// Unknown error → treat as transient (reschedule, don't count).
			sawTransient = true
			r.log.Warn("provider returned unexpected error",
				"provider", string(p.Name()), "error", err.Error())
		}
	}

	switch {
	case sawRateLimited:
		return nil, "", ErrRateLimited
	case sawTransient:
		return nil, "", ErrTransient
	case lastNotFound != nil:
		return nil, "", ErrNotFound
	}
	return nil, "", ErrNotFound
}

// ResolveCurrent walks the chain for a current price.
func (r *Resolver) ResolveCurrent(ctx context.Context, a asset.Asset) (*big.Int, Source, error) {
	var sawRateLimited, sawTransient bool

	for _, p := range r.providers {
		price, err := p.GetPrice(ctx, a)
		if err == nil {
			return price, p.Name(), nil
		}
		switch {
		case errors.Is(err, ErrRateLimited):
			sawRateLimited = true
		case errors.Is(err, ErrTransient):
			sawTransient = true
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrLowConfidence), errors.Is(err, ErrUnsupportedChain):
			// fall through
		default:
			sawTransient = true
		}
	}

	if sawRateLimited {
		return nil, "", ErrRateLimited
	}
	if sawTransient {
		return nil, "", ErrTransient
	}
	return nil, "", ErrNotFound
}
