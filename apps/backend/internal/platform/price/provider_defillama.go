// apps/backend/internal/platform/price/provider_defillama.go
package price

import (
	"context"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
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

func (p *DefiLlamaProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
	if a.ChainID == nil || a.ContractAddress == nil {
		return nil, ErrUnsupportedChain
	}
	return p.c.GetCurrentPrice(ctx, *a.ChainID, *a.ContractAddress)
}

func (p *DefiLlamaProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, at time.Time) (*HistoricalPrice, error) {
	if a.ChainID == nil || a.ContractAddress == nil {
		return nil, ErrUnsupportedChain
	}
	return p.c.GetHistoricalPrice(ctx, *a.ChainID, *a.ContractAddress, at)
}
