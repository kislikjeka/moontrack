package zerion_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/zerion"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

func testLogger() *logger.Logger {
	return logger.New("development", io.Discard)
}

// =============================================================================
// Auth Header Tests
// =============================================================================

func TestClient_AuthHeader(t *testing.T) {
	apiKey := "test-api-key-123"
	expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(apiKey+":"))

	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(zerion.TransactionResponse{})
	}))
	defer server.Close()

	client := zerion.NewClient(apiKey, testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "0xtest", "ethereum", time.Now())
	require.NoError(t, err)
	assert.Equal(t, expectedAuth, receivedAuth)
}

func TestClient_AcceptHeader(t *testing.T) {
	var receivedAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(zerion.TransactionResponse{})
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "0xtest", "ethereum", time.Now())
	require.NoError(t, err)
	assert.Equal(t, "application/json", receivedAccept)
}

// =============================================================================
// Query Parameters Tests
// =============================================================================

func TestClient_QueryParams(t *testing.T) {
	since := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	var receivedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(zerion.TransactionResponse{})
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "0xwallet", "ethereum", since)
	require.NoError(t, err)

	assert.Contains(t, receivedURL, "/wallets/0xwallet/transactions/")
	assert.Contains(t, receivedURL, "filter%5Bchain_ids%5D=ethereum")
	assert.Contains(t, receivedURL, "filter%5Bmin_mined_at%5D=1718452800000")
	assert.Contains(t, receivedURL, "filter%5Basset_types%5D=fungible")
	assert.Contains(t, receivedURL, "filter%5Btrash%5D=only_non_trash")
}

// =============================================================================
// Pagination Tests
// =============================================================================

func TestClient_Pagination(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")

		switch page {
		case 1:
			// First page — return data with a next link
			resp := zerion.TransactionResponse{
				Links: zerion.Links{Next: "NEXT_URL_PLACEHOLDER"},
				Data: []zerion.TransactionData{
					{ID: "tx1", Type: "transactions"},
				},
			}
			// Replace placeholder with actual server URL for absolute pagination link
			body, _ := json.Marshal(resp)
			bodyStr := strings.Replace(string(body), "NEXT_URL_PLACEHOLDER",
				r.URL.Scheme+"://"+r.Host+"/v1/wallets/0xtest/transactions/?page=2", 1)
			w.Write([]byte(bodyStr))
		case 2:
			// Second page — no next link
			resp := zerion.TransactionResponse{
				Data: []zerion.TransactionData{
					{ID: "tx2", Type: "transactions"},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			t.Fatal("unexpected request beyond page 2")
		}
	}))
	defer server.Close()

	// Fix: the next URL in the response must use the test server's URL
	var firstRequestCount int32
	paginationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := atomic.AddInt32(&firstRequestCount, 1)
		w.Header().Set("Content-Type", "application/json")

		if page == 1 {
			nextURL := "http://" + r.Host + "/v1/wallets/0xtest/transactions/?page=2"
			resp := map[string]interface{}{
				"links": map[string]string{"next": nextURL},
				"data": []map[string]interface{}{
					{"id": "tx1", "type": "transactions", "attributes": map[string]interface{}{}},
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			resp := map[string]interface{}{
				"links": map[string]string{},
				"data": []map[string]interface{}{
					{"id": "tx2", "type": "transactions", "attributes": map[string]interface{}{}},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer paginationServer.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(paginationServer.URL)

	txs, err := client.GetTransactions(context.Background(), "0xtest", "ethereum", time.Now())
	require.NoError(t, err)
	assert.Len(t, txs, 2)
	assert.Equal(t, "tx1", txs[0].ID)
	assert.Equal(t, "tx2", txs[1].ID)
	assert.Equal(t, int32(2), atomic.LoadInt32(&firstRequestCount))
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
		json.NewEncoder(w).Encode(zerion.TransactionResponse{
			Data: []zerion.TransactionData{{ID: "tx1", Type: "transactions"}},
		})
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	txs, err := client.GetTransactions(context.Background(), "0xtest", "ethereum", time.Now())
	require.NoError(t, err)
	assert.Len(t, txs, 1)
	// 2 rate-limited + 1 successful = 3 total requests
	assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

func TestClient_RateLimitExhaustion(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "0xtest", "ethereum", time.Now())
	require.Error(t, err)
	assert.True(t, zerion.IsRateLimitError(err))

	// initial attempt + maxRetries = 4 total requests
	assert.Equal(t, int32(4), atomic.LoadInt32(&requestCount))
}

func TestClient_RateLimitContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the backoff sleep returns context error
	cancel()

	_, err := client.GetTransactions(ctx, "0xtest", "ethereum", time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// =============================================================================
// Error Response Tests
// =============================================================================

func TestClient_NonOKResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal"}`))
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)

	_, err := client.GetTransactions(context.Background(), "0xtest", "ethereum", time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

// =============================================================================
// RateLimitError Type Tests
// =============================================================================

func TestRateLimitError(t *testing.T) {
	err := &zerion.RateLimitError{
		RetryAfter: time.Second * 4,
		Message:    "Zerion API rate limit exceeded",
	}

	assert.Contains(t, err.Error(), "Zerion API rate limit exceeded")
	assert.Contains(t, err.Error(), "4s")
	assert.True(t, zerion.IsRateLimitError(err))
	assert.False(t, zerion.IsRateLimitError(assert.AnError))
}
