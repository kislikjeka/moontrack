// apps/backend/internal/infra/gateway/defillama/client_test.go
package defillama_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/defillama"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

func TestClient_Current_ParsesPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/prices/current/")
		w.Write(fixture(t, "current.json"))
	}))
	defer srv.Close()

	c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
	p, err := c.GetCurrentPrice(context.Background(), "ethereum", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
	require.NoError(t, err)
	require.Equal(t, "100050000", p.String())
}

func TestClient_Historical_ParsesPriceAndTimestamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/prices/historical/")
		w.Write(fixture(t, "historical.json"))
	}))
	defer srv.Close()

	c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
	at := time.Unix(1710000000, 0).UTC()
	hp, err := c.GetHistoricalPrice(context.Background(), "ethereum", "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", at)
	require.NoError(t, err)
	require.Equal(t, "99980000", hp.PriceUSD.String())
	require.Equal(t, at, hp.Timestamp)
	require.InDelta(t, 0.98, hp.Confidence, 0.001)
}

func TestClient_LowConfidence_ReturnsErrLowConfidence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(fixture(t, "low_confidence.json"))
	}))
	defer srv.Close()

	c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
	_, err := c.GetCurrentPrice(context.Background(), "ethereum", "0xdeadbeef00000000000000000000000000000001")
	require.ErrorIs(t, err, price.ErrLowConfidence)
}

func TestClient_Empty_ReturnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"coins":{}}`))
	}))
	defer srv.Close()

	c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
	_, err := c.GetCurrentPrice(context.Background(), "ethereum", "0x00")
	require.ErrorIs(t, err, price.ErrNotFound)
}

func TestClient_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := defillama.NewClient(defillama.Config{BaseURL: srv.URL, MinConfidence: 0.9})
	_, err := c.GetCurrentPrice(context.Background(), "ethereum", "0x00")
	require.ErrorIs(t, err, price.ErrRateLimited)
}
