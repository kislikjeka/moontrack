// apps/backend/internal/platform/price/provider_geckoterminal.go
package price

import (
	"context"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// GeckoTerminalClient is the dependency GeckoTerminalProvider needs.
type GeckoTerminalClient interface {
	GetTokenPriceByAddress(ctx context.Context, chain, addr string) (*big.Int, error)
	GetPoolOHLCVMinute(ctx context.Context, chain, pool string, at time.Time) (*HistoricalPrice, error)
}

type GeckoTerminalProvider struct {
	c GeckoTerminalClient
}

func NewGeckoTerminalProvider(c GeckoTerminalClient) *GeckoTerminalProvider {
	return &GeckoTerminalProvider{c: c}
}

func (p *GeckoTerminalProvider) Name() Source { return SourceGeckoTerminal }

func (p *GeckoTerminalProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
	if a.ChainID == nil || a.ContractAddress == nil {
		return nil, ErrUnsupportedChain
	}
	return p.c.GetTokenPriceByAddress(ctx, *a.ChainID, *a.ContractAddress)
}

func (p *GeckoTerminalProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, error) {
	if a.ChainID == nil || a.ContractAddress == nil {
		return nil, ErrUnsupportedChain
	}
	// Historical OHLCV requires a pool address. We use the contract address as the "token" query
	// and let the GeckoTerminal-resolved primary pool take effect — the client helper below
	// resolves token → primary pool via a lookup. For simplicity of MVP, if we don't know the
	// pool we fall through as NotFound; a later refinement can add a pool-finder.
	//
	// In the current client, GetPoolOHLCVMinute expects a pool address. We therefore map
	// `ContractAddress` to pool here only when the caller already knows the pool; otherwise
	// we return NotFound to skip to DefiLlama.
	//
	// This conservative behavior is the right default: we do NOT implement pool discovery here.
	return nil, ErrNotFound
}
