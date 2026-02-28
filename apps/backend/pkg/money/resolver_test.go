package money

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSource is a test double for AssetDecimalSource.
type mockSource struct {
	data  map[string]int // key = "SYMBOL" or "SYMBOL:chainID"
	calls int32          // atomic call counter
}

func newMockSource(entries map[string]int) *mockSource {
	return &mockSource{data: entries}
}

func (m *mockSource) GetDecimalsBySymbol(_ context.Context, symbol, chainID string) (int, bool) {
	atomic.AddInt32(&m.calls, 1)
	key := symbol
	if chainID != "" {
		key = symbol + ":" + chainID
	}
	d, ok := m.data[key]
	return d, ok
}

func (m *mockSource) callCount() int {
	return int(atomic.LoadInt32(&m.calls))
}

func TestResolve_ReturnsFromFirstSource(t *testing.T) {
	src1 := newMockSource(map[string]int{"ETH": 18})
	src2 := newMockSource(map[string]int{"ETH": 9})

	r := NewDecimalResolver(src1, src2)
	d := r.Resolve(context.Background(), "ETH", "")

	assert.Equal(t, 18, d)
	assert.Equal(t, 1, src1.callCount(), "first source should be called")
	assert.Equal(t, 0, src2.callCount(), "second source should not be called")
}

func TestResolve_FallsThroughToSecondSource(t *testing.T) {
	src1 := newMockSource(map[string]int{}) // empty
	src2 := newMockSource(map[string]int{"USDC": 6})

	r := NewDecimalResolver(src1, src2)
	d := r.Resolve(context.Background(), "USDC", "")

	assert.Equal(t, 6, d)
	assert.Equal(t, 1, src1.callCount())
	assert.Equal(t, 1, src2.callCount())
}

func TestResolve_FallsBackToHardcodedMap(t *testing.T) {
	src1 := newMockSource(map[string]int{})

	r := NewDecimalResolver(src1)
	d := r.Resolve(context.Background(), "BTC", "")

	// BTC should be 8 from the hardcoded map
	assert.Equal(t, 8, d)
	assert.Equal(t, 1, src1.callCount())
}

func TestResolve_CacheHitAvoidsSources(t *testing.T) {
	src1 := newMockSource(map[string]int{"ETH": 18})

	r := NewDecimalResolver(src1)

	// First call populates cache
	d1 := r.Resolve(context.Background(), "ETH", "")
	assert.Equal(t, 18, d1)
	assert.Equal(t, 1, src1.callCount())

	// Second call should hit cache
	d2 := r.Resolve(context.Background(), "ETH", "")
	assert.Equal(t, 18, d2)
	assert.Equal(t, 1, src1.callCount(), "source should not be called on cache hit")
}

func TestResolve_ChainIDProducesDifferentCacheKey(t *testing.T) {
	src := newMockSource(map[string]int{
		"USDC":          6,
		"USDC:ethereum": 6,
		"USDC:solana":   9,
	})

	r := NewDecimalResolver(src)

	d1 := r.Resolve(context.Background(), "USDC", "ethereum")
	d2 := r.Resolve(context.Background(), "USDC", "solana")

	assert.Equal(t, 6, d1)
	assert.Equal(t, 9, d2)
}

func TestResolveSymbolOnly_DelegatesToResolve(t *testing.T) {
	src := newMockSource(map[string]int{"ETH": 18})
	r := NewDecimalResolver(src)

	d := r.ResolveSymbolOnly(context.Background(), "ETH")
	assert.Equal(t, 18, d)
}

func TestResolve_ConcurrentAccess(t *testing.T) {
	src := newMockSource(map[string]int{"ETH": 18, "BTC": 8})

	r := NewDecimalResolver(src)

	var wg sync.WaitGroup
	const goroutines = 100

	results := make([]int, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			symbol := "ETH"
			if idx%2 == 0 {
				symbol = "BTC"
			}
			results[idx] = r.Resolve(context.Background(), symbol, "")
		}(i)
	}

	wg.Wait()

	for i, d := range results {
		if i%2 == 0 {
			assert.Equal(t, 8, d, "BTC should resolve to 8")
		} else {
			assert.Equal(t, 18, d, "ETH should resolve to 18")
		}
	}
}

func TestResolve_NoSources_FallsBackToHardcoded(t *testing.T) {
	r := NewDecimalResolver()

	d := r.Resolve(context.Background(), "ETH", "")
	assert.Equal(t, 18, d)
}

func TestCacheKey_UpperCasesSymbol(t *testing.T) {
	require.Equal(t, "ETH", cacheKey("eth", ""))
	require.Equal(t, "ETH", cacheKey("ETH", ""))
	require.Equal(t, "ETH:ethereum", cacheKey("eth", "ethereum"))
}
