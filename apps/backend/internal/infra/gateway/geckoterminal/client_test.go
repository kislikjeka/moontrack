// apps/backend/internal/infra/gateway/geckoterminal/client_test.go
package geckoterminal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/geckoterminal"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

func TestClient_GetTokenPriceByAddress_ParsesPriceUSD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/networks/eth/tokens/multi/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "tokens_multi.json"))
	}))
	defer srv.Close()

	c := geckoterminal.NewClient(geckoterminal.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	p, err := c.GetTokenPriceByAddress(context.Background(), "eth", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
	require.NoError(t, err)
	// $1.0004 scaled 10^8 = 100040000
	require.Equal(t, "100040000", p.String())
}

func TestClient_429_ReturnsErrRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := geckoterminal.NewClient(geckoterminal.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.GetTokenPriceByAddress(context.Background(), "eth", "0x0")
	require.ErrorIs(t, err, price.ErrRateLimited)
}

func TestClient_404_ReturnsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := geckoterminal.NewClient(geckoterminal.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.GetTokenPriceByAddress(context.Background(), "eth", "0x0")
	require.ErrorIs(t, err, price.ErrNotFound)
}

func TestClient_GetHistoricalPrice_PicksNearestMinute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/ohlcv/minute")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "ohlcv_minute.json"))
	}))
	defer srv.Close()

	c := geckoterminal.NewClient(geckoterminal.Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ts := time.Unix(1744816620, 0).UTC()
	hp, err := c.GetPoolOHLCVMinute(context.Background(), "eth", "pool-xyz", ts)
	require.NoError(t, err)
	// close = 1.0004 → 100040000
	require.Equal(t, "100040000", hp.PriceUSD.String())
	require.Equal(t, ts, hp.Timestamp)
}
