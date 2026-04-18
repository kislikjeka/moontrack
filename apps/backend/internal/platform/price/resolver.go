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
// Error semantics (priority when no success):
//  1. Any success → return it.
//  2. Any provider answered with NotFound/LowConfidence/UnsupportedChain →
//     return ErrNotFound. This means we've exhausted the priority chain for
//     data at this moment — the attempt should count. A rate-limit on an
//     earlier provider is irrelevant to the pending lot's fate, because a
//     later provider positively confirmed "no data".
//  3. Else if any provider was transient → ErrTransient.
//  4. Else if any provider was rate-limited → ErrRateLimited. (Only reaches
//     here when ALL providers were rate-limited.)
//  5. Else ErrNotFound.
func (r *Resolver) ResolveHistorical(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, Source, error) {
	var sawRateLimited, sawTransient, sawNotFound bool
	// Preserve the first rate-limit error so errors.As can still recover a
	// *RateLimitedError (and its RetryAfter) when rate-limit wins.
	var rateLimitErr error

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
			sawNotFound = true
		case errors.Is(err, ErrRateLimited):
			sawRateLimited = true
			if rateLimitErr == nil {
				rateLimitErr = err
			}
		case errors.Is(err, ErrTransient):
			sawTransient = true
		default:
			// Unknown error → treat as transient (reschedule, don't count).
			sawTransient = true
			// Sanitize before logging: provider error strings can originate from
			// JSON/HTTP response bodies and may carry \n/\r/control bytes that
			// would let a hostile or misbehaving provider forge log records
			// downstream (log injection vector).
			r.log.Warn("provider returned unexpected error",
				"provider", sanitizeLogField(string(p.Name())),
				"error", sanitizeLogField(err.Error()))
		}
	}

	switch {
	case sawNotFound:
		// At least one provider positively answered "no data" — treat as
		// NotFound so the worker counts the attempt.
		return nil, "", ErrNotFound
	case sawTransient:
		return nil, "", ErrTransient
	case sawRateLimited:
		// Preserve the typed RateLimitedError so callers can still read RetryAfter.
		return nil, "", rateLimitErr
	}
	return nil, "", ErrNotFound
}

// ResolveCurrent walks the chain for a current price. Error priority matches
// ResolveHistorical: NotFound > Transient > RateLimited.
func (r *Resolver) ResolveCurrent(ctx context.Context, a asset.Asset) (*big.Int, Source, error) {
	var sawRateLimited, sawTransient, sawNotFound bool
	var rateLimitErr error

	for _, p := range r.providers {
		price, err := p.GetPrice(ctx, a)
		if err == nil {
			return price, p.Name(), nil
		}
		switch {
		case errors.Is(err, ErrNotFound), errors.Is(err, ErrLowConfidence), errors.Is(err, ErrUnsupportedChain):
			sawNotFound = true
		case errors.Is(err, ErrRateLimited):
			sawRateLimited = true
			if rateLimitErr == nil {
				rateLimitErr = err
			}
		case errors.Is(err, ErrTransient):
			sawTransient = true
		default:
			sawTransient = true
		}
	}

	switch {
	case sawNotFound:
		return nil, "", ErrNotFound
	case sawTransient:
		return nil, "", ErrTransient
	case sawRateLimited:
		return nil, "", rateLimitErr
	}
	return nil, "", ErrNotFound
}
