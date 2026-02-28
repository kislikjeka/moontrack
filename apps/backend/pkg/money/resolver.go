package money

import (
	"context"
	"strings"
	"sync"
)

// AssetDecimalSource provides decimal information for assets.
// Implementations query a specific data source (e.g., assets table, zerion_assets table).
type AssetDecimalSource interface {
	// GetDecimalsBySymbol returns the decimal count for an asset.
	// chainID may be empty for a chain-agnostic lookup.
	// Returns (decimals, true) if found, (0, false) if not.
	GetDecimalsBySymbol(ctx context.Context, symbol, chainID string) (int, bool)
}

// DecimalResolver resolves asset decimals via a cascading lookup:
// try each source in priority order, then fall back to the hardcoded map.
type DecimalResolver struct {
	sources []AssetDecimalSource
	cache   map[string]int
	mu      sync.RWMutex
}

// NewDecimalResolver creates a resolver with the given sources ordered by priority.
func NewDecimalResolver(sources ...AssetDecimalSource) *DecimalResolver {
	return &DecimalResolver{
		sources: sources,
		cache:   make(map[string]int),
	}
}

// Resolve returns the decimal count for a symbol on a specific chain.
func (r *DecimalResolver) Resolve(ctx context.Context, symbol, chainID string) int {
	key := cacheKey(symbol, chainID)

	// Check cache (read lock)
	r.mu.RLock()
	if d, ok := r.cache[key]; ok {
		r.mu.RUnlock()
		return d
	}
	r.mu.RUnlock()

	// Resolve from sources outside lock
	d := r.resolveFromSources(ctx, symbol, chainID)

	// Double-checked locking: another goroutine may have populated it
	r.mu.Lock()
	if existing, ok := r.cache[key]; ok {
		r.mu.Unlock()
		return existing
	}
	r.cache[key] = d
	r.mu.Unlock()
	return d
}

// ResolveSymbolOnly is a convenience method for chain-agnostic lookups.
func (r *DecimalResolver) ResolveSymbolOnly(ctx context.Context, symbol string) int {
	return r.Resolve(ctx, symbol, "")
}

// resolveFromSources tries each source in priority order, falling back to the hardcoded map.
func (r *DecimalResolver) resolveFromSources(ctx context.Context, symbol, chainID string) int {
	for _, src := range r.sources {
		if d, ok := src.GetDecimalsBySymbol(ctx, symbol, chainID); ok {
			return d
		}
	}
	return GetDecimals(symbol)
}

func cacheKey(symbol, chainID string) string {
	s := strings.ToUpper(symbol)
	if chainID != "" {
		return s + ":" + chainID
	}
	return s
}
