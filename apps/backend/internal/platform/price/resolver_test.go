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
	// If ALL providers are rate-limited, the resolver should return ErrRateLimited
	// (not ErrNotFound), so the worker reschedules instead of counting an attempt.
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrRateLimited},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrRateLimited)
}

func TestResolver_PreservesRateLimitedErrorWithRetryAfter(t *testing.T) {
	// The typed *RateLimitedError (with RetryAfter) must survive the resolver
	// when rate-limit is the winning classification.
	rle := &price.RateLimitedError{RetryAfter: 17 * time.Second}
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: rle},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrRateLimited)

	var got *price.RateLimitedError
	require.ErrorAs(t, err, &got)
	require.Equal(t, 17*time.Second, got.RetryAfter)
}

func TestResolver_NotFoundBeatsRateLimited(t *testing.T) {
	// Provider A is rate-limited, provider B says NotFound. The lot's fate is
	// sealed for now — provider B positively answered "no data", so the worker
	// should count an attempt (return ErrNotFound), not reschedule on A's 429.
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrRateLimited},
		&stubProvider{name: price.SourceDefiLlama, err: price.ErrNotFound},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrNotFound)
	// Must NOT be rate-limited.
	require.False(t, errors.Is(err, price.ErrRateLimited),
		"NotFound from a later provider must win over earlier rate-limit")
}

func TestResolver_TransientBeatsRateLimited(t *testing.T) {
	// If no provider answered NotFound but one hit a transient (5xx), we prefer
	// Transient over RateLimited so the worker uses the transient backoff.
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrRateLimited},
		&stubProvider{name: price.SourceDefiLlama, err: price.ErrTransient},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrTransient)
}

func TestResolver_NotFoundBeatsRateLimited_Current(t *testing.T) {
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrRateLimited},
		&stubProvider{name: price.SourceDefiLlama, err: price.ErrNotFound},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveCurrent(context.Background(), asset.Asset{})
	require.ErrorIs(t, err, price.ErrNotFound)
	require.False(t, errors.Is(err, price.ErrRateLimited))
}

func TestResolver_WrapsUnexpectedError(t *testing.T) {
	// Unknown provider error → treated as Transient so worker reschedules.
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: errors.New("boom")},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrTransient)
}
