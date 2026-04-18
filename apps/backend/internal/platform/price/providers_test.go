// apps/backend/internal/platform/price/providers_test.go
package price_test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/coingecko"
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

// fakeCGBridge implements price.CoinGeckoBridge.
type fakeCGBridge struct {
	curErr  error
	curOK   *big.Int
	histErr error
	histOK  *big.Int
}

func (f *fakeCGBridge) GetCurrentPriceByCoinGeckoIDNoFallback(ctx context.Context, id string) (*big.Int, error) {
	return f.curOK, f.curErr
}
func (f *fakeCGBridge) GetHistoricalPriceByCoinGeckoIDNoFallback(ctx context.Context, id string, at time.Time) (*big.Int, error) {
	return f.histOK, f.histErr
}

func TestCoinGeckoProvider_RateLimit_SurfacedAsRateLimitedError(t *testing.T) {
	// Simulate what asset.Service returns when CoinGecko responds 429: a wrapped
	// coingecko.RateLimitError. The provider must translate it into a
	// *price.RateLimitedError carrying the RetryAfter hint.
	rle := &coingecko.RateLimitError{RetryAfter: 42 * time.Second, Message: "rate limited"}
	wrapped := fmt.Errorf("failed to fetch current price: %w", rle)

	p := price.NewCoinGeckoProvider(&fakeCGBridge{curErr: wrapped})
	_, err := p.GetPrice(context.Background(), asset.Asset{CoinGeckoID: "bitcoin"})
	require.ErrorIs(t, err, price.ErrRateLimited)

	var outRLE *price.RateLimitedError
	require.ErrorAs(t, err, &outRLE)
	require.Equal(t, 42*time.Second, outRLE.RetryAfter)
}

func TestCoinGeckoProvider_HistoricalRateLimit_SurfacedAsRateLimitedError(t *testing.T) {
	rle := &coingecko.RateLimitError{RetryAfter: 10 * time.Second, Message: "rate limited"}
	wrapped := fmt.Errorf("failed to fetch historical price: %w", rle)

	p := price.NewCoinGeckoProvider(&fakeCGBridge{histErr: wrapped})
	_, err := p.GetHistoricalPrice(context.Background(), asset.Asset{CoinGeckoID: "bitcoin"}, time.Now())
	require.ErrorIs(t, err, price.ErrRateLimited)

	var outRLE *price.RateLimitedError
	require.ErrorAs(t, err, &outRLE)
	require.Equal(t, 10*time.Second, outRLE.RetryAfter)
}

func TestCoinGeckoProvider_GenericError_IsTransient(t *testing.T) {
	p := price.NewCoinGeckoProvider(&fakeCGBridge{curErr: errors.New("boom")})
	_, err := p.GetPrice(context.Background(), asset.Asset{CoinGeckoID: "bitcoin"})
	require.ErrorIs(t, err, price.ErrTransient)
}
