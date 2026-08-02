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
		&stubProvider{name: price.SourceDefiLlama, hist: newHP(200)},
	}, nil, logger.NewNoop())

	hp, src, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(100), hp.PriceUSD.Int64())
	require.Equal(t, price.SourceCoinGecko, src)
}

func TestResolver_FallsThroughOnNotFound(t *testing.T) {
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrNotFound},
		&stubProvider{name: price.SourceDefiLlama, hist: newHP(222)},
	}, nil, logger.NewNoop())

	hp, src, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(222), hp.PriceUSD.Int64())
	require.Equal(t, price.SourceDefiLlama, src)
}

func TestResolver_ReturnsNotFoundWhenAllMiss(t *testing.T) {
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrNotFound},
		&stubProvider{name: price.SourceDefiLlama, err: price.ErrNotFound},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrNotFound)
}

// TestResolver_TransientStaysTransientWithoutADeadProvider pins the reason the
// GeckoTerminal stub had to go. It answered NotFound unconditionally, and
// NotFound is priority 2 in ResolveHistorical — above transient. With the stub
// in the chain, a transient CoinGecko failure came back as ErrNotFound, which
// the backfill worker counts as an attempt and which walks the lot toward
// terminal "unpriceable". With only live providers, a transient failure stays
// transient, so the worker reschedules without burning an attempt.
func TestResolver_TransientStaysTransientWithoutADeadProvider(t *testing.T) {
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrTransient},
		&stubProvider{name: price.SourceDefiLlama, err: price.ErrTransient},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrTransient)
	require.False(t, errors.Is(err, price.ErrNotFound),
		"no provider positively answered 'no data' — the verdict must stay transient")
}

// TestResolver_AlwaysNotFoundProviderWouldPoisonTransientVerdict documents the
// exact failure mode the removal prevents: an always-NotFound stub anywhere in
// the chain converts a transient verdict into a counted attempt.
func TestResolver_AlwaysNotFoundProviderWouldPoisonTransientVerdict(t *testing.T) {
	deadStub := &stubProvider{name: price.SourceGeckoTerminal, err: price.ErrNotFound}
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: price.ErrTransient},
		deadStub,
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrNotFound,
		"this is why the dead provider must not be wired: it overrides the transient verdict")
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

// countingProvider tracks how many times it was called. Used to assert that
// a provider in cooldown is not invoked.
type countingProvider struct {
	name  price.Source
	err   error
	hist  *price.HistoricalPrice
	calls int
}

func (c *countingProvider) Name() price.Source { return c.name }
func (c *countingProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return c.hist.PriceUSD, nil
}
func (c *countingProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*price.HistoricalPrice, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return c.hist, nil
}

func TestResolver_CooldownSkipsProviderAfterThreeTransient(t *testing.T) {
	// Provider A fails with transient errors, provider B answers NotFound.
	// After three transient errors on A, A must be skipped on the 4th call.
	a := &countingProvider{name: price.SourceCoinGecko, err: price.ErrTransient}
	b := &countingProvider{name: price.SourceDefiLlama, err: price.ErrNotFound}
	r := price.NewResolver([]price.Provider{a, b}, nil, logger.NewNoop())

	for i := 0; i < 3; i++ {
		_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
		// With A transient + B NotFound, NotFound wins (BUG C semantics).
		require.ErrorIs(t, err, price.ErrNotFound, "iter %d", i)
	}
	require.Equal(t, 3, a.calls, "A should be called three times before cooldown")

	// 4th call: A is now in cooldown; only B is consulted.
	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrNotFound)
	require.Equal(t, 3, a.calls, "A must be skipped during cooldown")
	require.Equal(t, 4, b.calls, "B still receives every call")
}

func TestResolver_CooldownResetsOnSuccess(t *testing.T) {
	// Sequence: transient, transient, success, transient. Success in the
	// middle must reset the counter so the final transient doesn't cool
	// anyone down.
	wrapped := &sequenceProvider{name: price.SourceCoinGecko, errs: []error{
		price.ErrTransient, price.ErrTransient, nil, price.ErrTransient,
	}}
	r := price.NewResolver([]price.Provider{wrapped}, nil, logger.NewNoop())

	for i := 0; i < 4; i++ {
		_, _, _ = r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	}
	require.Equal(t, 4, wrapped.calls, "success must reset counter so 4th call still runs wrapped")
}

// sequenceProvider returns errors from a fixed sequence on each call.
type sequenceProvider struct {
	name  price.Source
	errs  []error
	hp    *price.HistoricalPrice
	calls int
}

func (s *sequenceProvider) Name() price.Source { return s.name }
func (s *sequenceProvider) GetPrice(ctx context.Context, a asset.Asset) (*big.Int, error) {
	s.calls++
	err := s.errs[(s.calls-1)%len(s.errs)]
	if err != nil {
		return nil, err
	}
	return big.NewInt(1), nil
}
func (s *sequenceProvider) GetHistoricalPrice(ctx context.Context, a asset.Asset, t time.Time) (*price.HistoricalPrice, error) {
	s.calls++
	err := s.errs[(s.calls-1)%len(s.errs)]
	if err != nil {
		return nil, err
	}
	if s.hp == nil {
		return &price.HistoricalPrice{PriceUSD: big.NewInt(1), Timestamp: time.Now(), Confidence: 1}, nil
	}
	return s.hp, nil
}

func TestResolver_WrapsUnexpectedError(t *testing.T) {
	// Unknown provider error → treated as Transient so worker reschedules.
	r := price.NewResolver([]price.Provider{
		&stubProvider{name: price.SourceCoinGecko, err: errors.New("boom")},
	}, nil, logger.NewNoop())

	_, _, err := r.ResolveHistorical(context.Background(), asset.Asset{}, time.Now())
	require.ErrorIs(t, err, price.ErrTransient)
}
