package zerion

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kislikjeka/moontrack/pkg/logger"
)

const (
	defaultBaseURL = "https://api.zerion.io/v1"
	requestTimeout = 30 * time.Second
	maxRetries     = 3
)

// Client is an HTTP client for the Zerion REST API
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
	logger     *logger.Logger
}

// NewClient creates a new Zerion API client
func NewClient(apiKey string, log *logger.Logger) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		baseURL: defaultBaseURL,
		logger:  log.WithField("component", "zerion"),
	}
}

// SetBaseURL overrides the default base URL (useful for testing)
func (c *Client) SetBaseURL(url string) {
	c.baseURL = url
}

// doRequest performs an authenticated HTTP request with rate-limit retry.
// It retries up to maxRetries times with exponential backoff (1s, 2s, 4s) on 429 responses.
func (c *Client) doRequest(ctx context.Context, method, reqURL string, params url.Values) ([]byte, error) {
	// Append query params if provided
	if len(params) > 0 {
		parsed, err := url.Parse(reqURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse URL: %w", err)
		}
		existing := parsed.Query()
		for k, vals := range params {
			for _, v := range vals {
				existing.Add(k, v)
			}
		}
		parsed.RawQuery = existing.Encode()
		reqURL = parsed.String()
	}

	backoff := time.Second
	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.logger.Debug("API request", "method", method, "url", reqURL, "attempt", attempt)
		attemptStart := time.Now()

		req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Zerion uses Basic auth: base64(apiKey + ":")
		auth := base64.StdEncoding.EncodeToString([]byte(c.apiKey + ":"))
		req.Header.Set("Authorization", "Basic "+auth)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to execute request: %w", err)
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("failed to read response body: %w", readErr)
		}

		if resp.StatusCode == http.StatusOK {
			c.logger.Debug("API response", "status_code", resp.StatusCode, "duration_ms", time.Since(attemptStart).Milliseconds())
			return body, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt == maxRetries {
				c.logger.Error("rate limit exhausted", "attempts", maxRetries+1)
				return nil, &RateLimitError{
					RetryAfter: backoff,
					Message:    "Zerion API rate limit exceeded after retries",
				}
			}
			c.logger.Warn("rate limited, retrying", "attempt", attempt, "backoff_ms", backoff.Milliseconds())
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
				continue
			}
		}

		c.logger.Error("API error", "status_code", resp.StatusCode)
		return nil, fmt.Errorf("Zerion API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Should not be reached, but guard against it
	return nil, fmt.Errorf("Zerion API: exhausted retries")
}

// GetTransactions fetches decoded transactions for an address on a specific chain since a given time.
// It handles pagination by following the absolute Links.Next URL.
func (c *Client) GetTransactions(ctx context.Context, address, chainID string, since time.Time) ([]TransactionData, error) {
	fetchStart := time.Now()
	reqURL := fmt.Sprintf("%s/wallets/%s/transactions/", c.baseURL, address)

	params := url.Values{}
	params.Set("filter[chain_ids]", chainID)
	params.Set("filter[min_mined_at]", since.UTC().Format(time.RFC3339))
	params.Set("filter[asset_types]", "fungible")
	params.Set("filter[trash]", "only_non_trash")

	var allTxs []TransactionData

	for {
		body, err := c.doRequest(ctx, http.MethodGet, reqURL, params)
		if err != nil {
			return nil, fmt.Errorf("GetTransactions failed: %w", err)
		}

		var resp TransactionResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("failed to decode Zerion response: %w", err)
		}

		allTxs = append(allTxs, resp.Data...)

		if resp.Links.Next == "" {
			break
		}

		// Zerion's Links.Next is an absolute URL — use it directly
		reqURL = resp.Links.Next
		params = nil // params are already embedded in the absolute URL
	}

	c.logger.Info("transactions fetched", "address", address, "count", len(allTxs), "duration_ms", time.Since(fetchStart).Milliseconds())
	return allTxs, nil
}

// RateLimitError represents a rate limit error from Zerion API
type RateLimitError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s (retry after %s)", e.Message, e.RetryAfter)
}

// IsRateLimitError checks if an error is (or wraps) a Zerion rate limit error
func IsRateLimitError(err error) bool {
	var rle *RateLimitError
	return errors.As(err, &rle)
}
