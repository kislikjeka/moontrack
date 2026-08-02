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
