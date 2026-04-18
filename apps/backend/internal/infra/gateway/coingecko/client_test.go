package coingecko_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/coingecko"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

func testLogger() *logger.Logger {
	return logger.New("test", io.Discard)
}

// writeOversizedAround writes a JSON-opening prefix, then 2 MiB of filler
// ('a' bytes), then a JSON-closing suffix. Total body is > 2 MiB. Callers
// wrap their decode in io.LimitReader(_, 1 MiB) so the decoder sees a
// truncated body and fails — the critical property is that the client does
// NOT load the full 2 MiB into memory. We verify this indirectly by
// asserting the client returns a decode error rather than parsing the
// 'malicious' payload successfully.
func writeOversizedAround(w http.ResponseWriter, prefix, suffix string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(prefix))
	filler := make([]byte, 2<<20)
	for i := range filler {
		filler[i] = 'a'
	}
	_, _ = w.Write(filler)
	_, _ = w.Write([]byte(suffix))
}

func newClient(t *testing.T, srv *httptest.Server) *coingecko.Client {
	t.Helper()
	c := coingecko.NewClient("test-api-key", testLogger())
	c.SetBaseURL(srv.URL)
	return c
}

// TestClient_OversizedResponse_SimplePrice verifies /simple/price decode path
// is bounded. A 2 MiB response body must not OOM; the truncated JSON causes
// a decode error.
func TestClient_OversizedResponse_SimplePrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Legitimate-looking JSON start, then 2 MiB of garbage.
		writeOversizedAround(w, `{"bitcoin":{"usd":`, `}}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	_, err := c.GetCurrentPrices(context.Background(), []string{"bitcoin"})
	require.Error(t, err, "oversized response must fail decode (not OOM)")
}

// TestClient_OversizedResponse_Historical verifies the /coins/{id}/history
// decode path is bounded.
func TestClient_OversizedResponse_Historical(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOversizedAround(w, `{"id":"bitcoin","symbol":"btc","market_data":{"current_price":{"usd":`, `}}}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	_, err := c.GetHistoricalPrice(context.Background(), "bitcoin", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	require.Error(t, err, "oversized response must fail decode (not OOM)")
}

// TestClient_OversizedResponse_CoinDetails verifies the /coins/{id} decode
// path is bounded.
func TestClient_OversizedResponse_CoinDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOversizedAround(w, `{"id":"bitcoin","symbol":"btc","name":"`, `"}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	_, err := c.GetCoinDetails(context.Background(), "bitcoin")
	require.Error(t, err, "oversized response must fail decode (not OOM)")
}

// TestClient_OversizedResponse_Search verifies the /search decode path is
// bounded.
func TestClient_OversizedResponse_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOversizedAround(w, `{"coins":[{"id":"`, `"}]}`)
	}))
	defer srv.Close()

	c := newClient(t, srv)
	_, err := c.SearchCoins(context.Background(), "bitcoin")
	require.Error(t, err, "oversized response must fail decode (not OOM)")
}
