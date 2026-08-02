// apps/backend/internal/platform/price/provider_defillama.go
package price

import (
	"context"
	"math/big"
	"time"
)

// DefiLlamaClient is the dependency DefiLlamaProvider needs.
type DefiLlamaClient interface {
	GetCurrentPrice(ctx context.Context, chain, addr string) (*big.Int, error)
	GetHistoricalPrice(ctx context.Context, chain, addr string, at time.Time) (*HistoricalPrice, error)
}

// DefiLlamaProvider wraps the DefiLlama gateway client as a price.Provider.
type DefiLlamaProvider struct {
	c DefiLlamaClient
}

func NewDefiLlamaProvider(c DefiLlamaClient) *DefiLlamaProvider {
	return &DefiLlamaProvider{c: c}
}

func (p *DefiLlamaProvider) Name() Source { return SourceDefiLlama }

// addressable returns the (chain, contract) pair to query, or false when this
// provider cannot address the identity.
//
// DefiLlama's coins API is keyed on `{chain}:{contract}` — a real contract
// address. The native coin has none: the registry spells it (chain, 'native'),
// and DefiLlama has a separate `coingecko:{slug}` addressing scheme for coins
// rather than a per-chain native key. Passing the sentinel through would send
// the literal string `ethereum:native`, which the API cannot resolve; it would
// come back as a missing key, i.e. ErrNotFound.
//
// That distinction matters to the resolver, not just to tidiness. ErrNotFound is
// a POSITIVE "no data" answer and outranks transient errors, so it terminates
// the chain and spends one of the backfill job's finite attempts. Answering
// ErrUnsupportedChain here is the truthful verdict — this provider cannot
// address this asset — and it costs the job nothing while CoinGecko, which is
// ahead of DefiLlama in the chain and holds the native slug, does the pricing.
//
// Native coins therefore get prices via CoinGeckoProvider (#59); see the
// registry's coingecko_id column, which is what carries that slug.
func addressable(a Asset) (chain, contract string, ok bool) {
	if a.Chain == "" || a.Contract == "" || a.Contract == nativeContract {
		return "", "", false
	}
	return a.Chain, a.Contract, true
}

func (p *DefiLlamaProvider) GetPrice(ctx context.Context, a Asset) (*big.Int, error) {
	chain, contract, ok := addressable(a)
	if !ok {
		return nil, ErrUnsupportedChain
	}
	return p.c.GetCurrentPrice(ctx, chain, contract)
}

func (p *DefiLlamaProvider) GetHistoricalPrice(ctx context.Context, a Asset, at time.Time) (*HistoricalPrice, error) {
	chain, contract, ok := addressable(a)
	if !ok {
		return nil, ErrUnsupportedChain
	}
	return p.c.GetHistoricalPrice(ctx, chain, contract, at)
}
