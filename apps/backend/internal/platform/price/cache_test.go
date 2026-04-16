package price

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeRedis is a minimal in-memory implementation of the small interface the cache needs.
type fakeRedis struct {
	m map[string]string
}

func (f *fakeRedis) Get(ctx context.Context, key string) (string, bool, error) {
	v, ok := f.m[key]
	return v, ok, nil
}
func (f *fakeRedis) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if f.m == nil {
		f.m = map[string]string{}
	}
	f.m[key] = value
	return nil
}

func TestCache_KeyFormat_MinuteBucket(t *testing.T) {
	c := NewCache(&fakeRedis{}, 30*24*time.Hour)
	id := uuid.New()
	ts := time.Date(2026, 4, 16, 14, 37, 45, 0, time.UTC)
	k := c.historicalKey(SourceGeckoTerminal, id, ts)
	require.Equal(t, "price:hist:geckoterminal:"+id.String()+":2026-04-16T14:37Z", k)
}

func TestCache_WriteReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := NewCache(&fakeRedis{}, time.Hour)
	id := uuid.New()
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	hp := &HistoricalPrice{PriceUSD: big.NewInt(12345), Timestamp: ts, Confidence: 1}

	require.NoError(t, c.PutHistorical(ctx, SourceDefiLlama, id, ts, hp))

	got, ok, err := c.GetHistorical(ctx, SourceDefiLlama, id, ts)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "12345", got.PriceUSD.String())
	require.Equal(t, float64(1), got.Confidence)
	require.Equal(t, ts.UTC(), got.Timestamp.UTC())
}
