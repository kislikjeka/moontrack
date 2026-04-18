// apps/backend/internal/platform/price/resolver.go
package price

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Cooldown thresholds applied per-provider. Tuned for a single-worker setup
// at ~1 rps — three consecutive transient errors is a strong signal of an
// ongoing outage, so park the provider for 5 minutes and let the chain fall
// through to the next one.
const (
	providerCooldownThreshold = 3
	providerCooldownDuration  = 5 * time.Minute
)

// providerCooldownState tracks in-memory health for a single provider.
// Protected by Resolver.mu.
type providerCooldownState struct {
	consecutiveTransient int
	cooldownUntil        time.Time
}

// Resolver walks an ordered provider chain. The first non-error success wins.
type Resolver struct {
	providers []Provider
	cache     *Cache
	log       *logger.Logger

	// In-memory per-provider cooldown. Scoped to the process; multi-worker
	// deployments still need a distributed bucket (see FOLLOWUP-PRICE-WORKER-SCALE).
	mu        sync.Mutex
	cooldowns map[Source]*providerCooldownState
	now       func() time.Time // override for tests
}

// NewResolver returns a Resolver. cache may be nil; providers order defines priority.
func NewResolver(providers []Provider, cache *Cache, log *logger.Logger) *Resolver {
	return &Resolver{
		providers: providers,
		cache:     cache,
		log:       log.WithField("component", "price_resolver"),
		cooldowns: make(map[Source]*providerCooldownState, len(providers)),
		now:       time.Now,
	}
}

// isCoolingDown reports whether the provider is currently parked. Also trims
// the cooldown once it has expired.
func (r *Resolver) isCoolingDown(name Source) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.cooldowns[name]
	if !ok {
		return false
	}
	if st.cooldownUntil.IsZero() {
		return false
	}
	if r.now().Before(st.cooldownUntil) {
		return true
	}
	// Cooldown expired — clear window but keep the counter at 0 so the next
	// error starts a fresh streak.
	st.cooldownUntil = time.Time{}
	st.consecutiveTransient = 0
	return false
}

// recordProviderResult updates cooldown state for a provider based on its last
// outcome. Only transient errors count toward the streak; success resets.
// Non-transient non-success errors (NotFound/RateLimited) are neutral.
func (r *Resolver) recordProviderResult(name Source, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.cooldowns[name]
	if !ok {
		st = &providerCooldownState{}
		r.cooldowns[name] = st
	}

	switch {
	case err == nil:
		st.consecutiveTransient = 0
		st.cooldownUntil = time.Time{}
	case isTransientClass(err):
		st.consecutiveTransient++
		if st.consecutiveTransient >= providerCooldownThreshold {
			st.cooldownUntil = r.now().Add(providerCooldownDuration)
			st.consecutiveTransient = 0
			// Intentionally no log field sanitization needed: Source is a
			// controlled constant set, not provider-controlled input.
			r.log.Warn("provider parked in cooldown",
				"provider", string(name),
				"duration", providerCooldownDuration.String())
		}
	default:
		// NotFound / RateLimited / LowConfidence / UnsupportedChain — neutral.
	}
}

// isTransientClass reports whether an error represents a transient failure
// that should count toward the per-provider cooldown streak. Unknown errors
// are treated as transient to match the resolver's historical behavior.
func isTransientClass(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrLowConfidence) ||
		errors.Is(err, ErrUnsupportedChain) ||
		errors.Is(err, ErrRateLimited) {
		return false
	}
	// Treat ErrTransient and anything else as transient-class for cooldown.
	return true
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
		// Skip providers that are cooling down from a recent transient storm.
		if r.isCoolingDown(p.Name()) {
			continue
		}

		if r.cache != nil && a.ID != (asset.Asset{}).ID {
			if hp, ok, _ := r.cache.GetHistorical(ctx, p.Name(), a.ID, at); ok {
				return hp, p.Name(), nil
			}
		}

		hp, err := p.GetHistoricalPrice(ctx, a, at)
		// Update cooldown state with the raw result before re-classifying.
		r.recordProviderResult(p.Name(), err)
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
		if r.isCoolingDown(p.Name()) {
			continue
		}
		price, err := p.GetPrice(ctx, a)
		r.recordProviderResult(p.Name(), err)
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
