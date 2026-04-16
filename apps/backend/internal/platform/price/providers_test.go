// apps/backend/internal/platform/price/providers_test.go
package price_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/stretchr/testify/require"
)

type fakeGTClient struct {
	pricer func(chain, addr string) (*big.Int, error)
	hist   func(chain, pool string, at time.Time) (*price.HistoricalPrice, error)
}

func (f *fakeGTClient) GetTokenPriceByAddress(ctx context.Context, chain, addr string) (*big.Int, error) {
	return f.pricer(chain, addr)
}
func (f *fakeGTClient) GetPoolOHLCVMinute(ctx context.Context, chain, pool string, at time.Time) (*price.HistoricalPrice, error) {
	return f.hist(chain, pool, at)
}

func TestGeckoTerminalProvider_RequiresContractAddress(t *testing.T) {
	p := price.NewGeckoTerminalProvider(&fakeGTClient{
		pricer: func(chain, addr string) (*big.Int, error) { return big.NewInt(1), nil },
	})
	// Native L1 asset (no contract address) → not supported.
	_, err := p.GetPrice(context.Background(), asset.Asset{Symbol: "BTC"})
	require.ErrorIs(t, err, price.ErrUnsupportedChain)
}

func TestGeckoTerminalProvider_ReturnsPriceForTokens(t *testing.T) {
	p := price.NewGeckoTerminalProvider(&fakeGTClient{
		pricer: func(chain, addr string) (*big.Int, error) { return big.NewInt(100050000), nil },
	})
	chain := "ethereum"
	addr := "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
	got, err := p.GetPrice(context.Background(), asset.Asset{ChainID: &chain, ContractAddress: &addr})
	require.NoError(t, err)
	require.Equal(t, "100050000", got.String())
}
