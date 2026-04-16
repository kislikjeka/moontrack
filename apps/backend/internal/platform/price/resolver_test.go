// apps/backend/internal/platform/price/resolver_test.go
package price_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/stretchr/testify/require"
)

type stubProvider struct {
	name price.Source
	hist *price.HistoricalPrice
	err  error
}

func (s *stubProvider) Name() price.Source { return s.name }
func (s *stubProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.hist.PriceUSD, nil
}
func (s *stubProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*price.HistoricalPrice, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.hist, nil
}

func newHP(p int64) *price.HistoricalPrice {
	return &price.HistoricalPrice{PriceUSD: big.NewInt(p), Timestamp: time.Now(), Confidence: 1}
}

func TestResolver_FirstProviderWins(t *testing.T) {
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, hist: newHP(100)},
		&stubProvider{name: price.SourceGeckoTerminal, hist: newHP(200)},
	}, nil, logger.NewNoop())

	hp, src, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(100), hp.PriceUSD.Int64())
	require.Equal(t, price.SourceCoinGecko, src)
}

func TestResolver_FallsThroughOnNotFound(t *testing.T) {
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrNotFound},
		&stubProvider{name: price.SourceGeckoTerminal, hist: newHP(222)},
	}, nil, logger.NewNoop())

	hp, src, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(222), hp.PriceUSD.Int64())
	require.Equal(t, price.SourceGeckoTerminal, src)
}

func TestResolver_ReturnsNotFoundWhenAllMiss(t *testing.T) {
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrNotFound},
		&stubProvider{name: price.SourceGeckoTerminal, err: price.ErrNotFound},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrNotFound)
}

func TestResolver_PreservesRateLimitedError(t *testing.T) {
	// If all providers are rate-limited, the resolver should return ErrRateLimited
	// (not ErrNotFound), so the worker reschedules instead of counting an attempt.
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrRateLimited},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrRateLimited)
}

func TestResolver_WrapsUnexpectedError(t *testing.T) {
	// Unknown provider error → treated as Transient so worker reschedules.
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: errors.New("boom")},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrTransient)
}
