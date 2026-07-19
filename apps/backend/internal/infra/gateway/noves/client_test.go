package noves_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/noves"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

func testLogger() *logger.Logger {
	return logger.New("development", io.Discard)
}

// =============================================================================
// Auth / Header Tests
// =============================================================================

func TestClient_AuthHeader(t *testing.T) {
	apiKey := "test-api-key-123"

	var receivedKey, receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("apiKey")
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(noves.TransactionsResponse{})
	}))
	defer server.Close()

	client := noves.NewClient(apiKey, testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.NoError(t, err)
	assert.Equal(t, apiKey, receivedKey, "Noves uses an apiKey header")
	assert.Empty(t, receivedAuth, "Noves must not send Basic auth")
}

func TestClient_AcceptHeader(t *testing.T) {
	var receivedAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(noves.TransactionsResponse{})
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.NoError(t, err)
	assert.Equal(t, "application/json", receivedAccept)
}

// =============================================================================
// URL / Query Parameter Tests
// =============================================================================

func TestClient_URLAndParams(t *testing.T) {
	since := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	var receivedPath, receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(noves.TransactionsResponse{})
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "eth", "0xwallet", since)
	require.NoError(t, err)

	assert.Equal(t, "/evm/eth/txs/0xwallet", receivedPath)
	assert.Contains(t, receivedQuery, "sort=asc")
	assert.Contains(t, receivedQuery, "pageSize=")
	assert.Contains(t, receivedQuery, "startTimestamp=1718452800000")
}

func TestClient_ZeroSinceOmitsStartTimestamp(t *testing.T) {
	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(noves.TransactionsResponse{})
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xwallet", time.Time{})
	require.NoError(t, err)

	assert.NotContains(t, receivedQuery, "startTimestamp", "zero since must omit startTimestamp")
	assert.Contains(t, receivedQuery, "sort=asc")
}

// =============================================================================
// Pagination Tests
// =============================================================================

func TestClient_Pagination(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")

		if page == 1 {
			nextURL := "http://" + r.Host + "/evm/base/txs/0xtest?sort=asc&pageSize=100&pageNumber=2"
			resp := map[string]interface{}{
				"hasNextPage": true,
				"nextPageUrl": nextURL,
				"items": []map[string]interface{}{
					{"rawTransactionData": map[string]interface{}{"transactionHash": "0xtx1"}},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp := map[string]interface{}{
			"hasNextPage": false,
			"nextPageUrl": "",
			"items": []map[string]interface{}{
				{"rawTransactionData": map[string]interface{}{"transactionHash": "0xtx2"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	txs, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.NoError(t, err)
	require.Len(t, txs, 2)
	assert.Equal(t, "0xtx1", txs[0].RawTransactionData.TransactionHash)
	assert.Equal(t, "0xtx2", txs[1].RawTransactionData.TransactionHash)
	assert.Equal(t, int32(2), atomic.LoadInt32(&requestCount))
}

func TestClient_PaginationStopsWhenHasNextPageFalse(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		// hasNextPage false even though a nextPageUrl is present → must stop.
		resp := map[string]interface{}{
			"hasNextPage": false,
			"nextPageUrl": "http://" + r.Host + "/should/not/follow",
			"items":       []map[string]interface{}{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount))
}

// =============================================================================
// Rate Limit Tests
// =============================================================================

func TestClient_RateLimitRetry(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(noves.TransactionsResponse{})
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

func TestClient_RateLimitExhaustion(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.Error(t, err)
	assert.True(t, noves.IsRateLimitError(err))
	assert.Equal(t, int32(4), atomic.LoadInt32(&requestCount)) // initial + maxRetries
}

func TestClient_RateLimitContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so the backoff sleep returns context error

	_, err := client.GetTransactions(ctx, "base", "0xtest", time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// =============================================================================
// Error Response Tests
// =============================================================================

func TestClient_NonOKResponse(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount), "4xx must not be retried")
}

// =============================================================================
// Transient 5xx / Network Error Retry Tests (mirror MT-SYNC-16)
// =============================================================================

func TestClient_ServerErrorRetrySucceeds(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count <= 2 {
			w.WriteHeader(http.StatusBadGateway) // 502
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(noves.TransactionsResponse{})
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

func TestClient_ServerErrorExhaustion(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusInternalServerError) // 500
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
	assert.False(t, noves.IsRateLimitError(err))
	assert.Equal(t, int32(4), atomic.LoadInt32(&requestCount))
}

func TestClient_NetworkErrorRetrySucceeds(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count <= 2 {
			hj, ok := w.(http.Hijacker)
			require.True(t, ok, "test server must support hijacking")
			conn, _, err := hj.Hijack()
			require.NoError(t, err)
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(noves.TransactionsResponse{})
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

// =============================================================================
// Oversized Response Test
// =============================================================================

func TestClient_OversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"rawTransactionData":{"transactionHash":"`))
		filler := make([]byte, 20<<20) // 20 MiB — exceeds 16 MiB bound
		for i := range filler {
			filler[i] = 'a'
		}
		_, _ = w.Write(filler)
		_, _ = w.Write([]byte(`"}}]}`))
	}))
	defer server.Close()

	client := noves.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "base", "0xtest", time.Now())
	require.Error(t, err, "oversized response must fail decode (not OOM)")
	assert.Contains(t, err.Error(), "decode")
}

// =============================================================================
// RateLimitError Type Test
// =============================================================================

func TestRateLimitError(t *testing.T) {
	err := &noves.RateLimitError{
		RetryAfter: time.Second * 4,
		Message:    "Noves API rate limit exceeded",
	}
	assert.Contains(t, err.Error(), "Noves API rate limit exceeded")
	assert.Contains(t, err.Error(), "4s")
	assert.True(t, noves.IsRateLimitError(err))
	assert.False(t, noves.IsRateLimitError(assert.AnError))
}
