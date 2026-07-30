package noves

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kislikjeka/moontrack/pkg/logger"
)

const (
	defaultBaseURL = "https://translate.noves.fi"
	requestTimeout = 30 * time.Second
	maxRetries     = 3
	// defaultPageSize is the transactions-per-page requested from Noves. The API
	// validates pageSize ∈ [1, 50] and rejects anything larger with HTTP 400, so
	// 50 is the maximum we can ask for.
	defaultPageSize = 50

	// maxResponseBytes bounds the size of a single Noves API response body we
	// will read into memory. Protects against a hostile/misbehaving upstream
	// returning gigantic payloads (OOM / DoS vector). 16 MiB matches the Zerion
	// client: a paginated page of classified transactions with nested transfer
	// arrays can legitimately be large, but never this large.
	maxResponseBytes int64 = 16 << 20 // 16 MiB
)

// Client is an HTTP client for the Noves Translate REST API (v2).
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
	logger     *logger.Logger
}

// NewClient creates a new Noves Translate API client.
func NewClient(apiKey string, log *logger.Logger) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		baseURL: defaultBaseURL,
		logger:  log.WithField("component", "noves"),
	}
}

// SetBaseURL overrides the default base URL (useful for testing).
func (c *Client) SetBaseURL(u string) {
	c.baseURL = u
}

// doRequest performs an authenticated HTTP request with transient-error retry.
// It retries up to maxRetries times with exponential backoff (1s, 2s, 4s) on
// transient failures: HTTP 429, HTTP 5xx, and network errors from the
// transport. Non-transient failures (4xx other than 429) are returned
// immediately. This mirrors the hardened Zerion retry structure (MT-SYNC-16).
func (c *Client) doRequest(ctx context.Context, method, reqURL string, params url.Values) ([]byte, error) {
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

	// retryWait sleeps for the current backoff (respecting context cancellation)
	// then doubles it. Returns an error only if the context is cancelled while
	// waiting; a nil return means the caller should retry. Callers must check
	// attempt < maxRetries before invoking it.
	retryWait := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
			return nil
		}
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.logger.Debug("API request", "method", method, "url", reqURL, "attempt", attempt)
		attemptStart := time.Now()

		req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// Noves uses an apiKey header (NOT Basic auth).
		req.Header.Set("apiKey", c.apiKey)
		req.Header.Set("accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Network/transport error (dropped connection, DNS, etc.) — transient.
			if attempt == maxRetries {
				c.logger.Error("request failed after retries", "attempts", maxRetries+1, "error", err)
				return nil, fmt.Errorf("failed to execute request: %w", err)
			}
			c.logger.Warn("request failed, retrying", "attempt", attempt, "backoff_ms", backoff.Milliseconds(), "error", err)
			if werr := retryWait(); werr != nil {
				return nil, werr
			}
			continue
		}

		// Bound the read to guard against a hostile/misbehaving upstream. Overflow
		// yields a truncated body that fails to decode (deterministic, not OOM).
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
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
					Message:    "Noves API rate limit exceeded after retries",
				}
			}
			c.logger.Warn("rate limited, retrying", "attempt", attempt, "backoff_ms", backoff.Milliseconds())
			if werr := retryWait(); werr != nil {
				return nil, werr
			}
			continue
		}

		// Server-side errors (5xx) are transient — retry with the same backoff.
		if resp.StatusCode >= 500 {
			if attempt == maxRetries {
				c.logger.Error("server error exhausted retries", "status_code", resp.StatusCode, "attempts", maxRetries+1)
				return nil, fmt.Errorf("Noves API error: status %d, body: %s", resp.StatusCode, string(body))
			}
			c.logger.Warn("server error, retrying", "attempt", attempt, "status_code", resp.StatusCode, "backoff_ms", backoff.Milliseconds())
			if werr := retryWait(); werr != nil {
				return nil, werr
			}
			continue
		}

		// Client errors (4xx other than 429) are not transient — fail immediately.
		c.logger.Error("API error", "status_code", resp.StatusCode)
		return nil, fmt.Errorf("Noves API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	// Should not be reached, but guard against it.
	return nil, fmt.Errorf("Noves API: exhausted retries")
}

// GetTransactions fetches classified transactions for a single chain and
// address, oldest-first, accumulating every page into one slice. Prefer
// StreamTransactions when the caller can persist incrementally — a deep sync
// interrupted partway through loses nothing there, while this method discards
// all collected pages on any error.
func (c *Client) GetTransactions(ctx context.Context, chain, address string, since time.Time) ([]Transaction, error) {
	var allTxs []Transaction
	err := c.StreamTransactions(ctx, chain, address, since, func(page []Transaction) error {
		allTxs = append(allTxs, page...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return allTxs, nil
}

// StreamTransactions fetches classified transactions for a single chain and
// address oldest-first, invoking onPage once per page in ascending order. The
// chain is a Noves short slug (e.g. "eth", "base"). It paginates by following
// nextPageUrl until hasNextPage is false.
//
// Ordering and windowing were confirmed against the live API (issue #29 probe):
//   - sort=asc is honored natively, so no client-side reordering is needed;
//   - pagination direction follows sort — an ascending nextPageUrl advances
//     startBlock forward and carries ignoreTransactions for the boundary block,
//     so pages arrive strictly oldest→newest;
//   - startTimestamp is INCLUSIVE (>=): a transaction mined exactly at `since`
//     is returned. Callers therefore re-see their cursor transaction and rely on
//     idempotent storage to absorb it, rather than nudging the boundary forward
//     (which would skip same-timestamp siblings).
//
// startTimestamp is expressed in SECONDS. Sending milliseconds makes the API
// read it as a far-future instant and reject the request with HTTP 400.
//
// If onPage returns an error, pagination stops and that error is returned to the
// caller unwrapped, so a caller can use it as a deliberate early exit.
func (c *Client) StreamTransactions(ctx context.Context, chain, address string, since time.Time, onPage func([]Transaction) error) error {
	fetchStart := time.Now()
	reqURL := fmt.Sprintf("%s/evm/%s/txs/%s", c.baseURL, chain, address)

	params := url.Values{}
	params.Set("pageSize", strconv.Itoa(defaultPageSize))
	params.Set("sort", "asc")
	if !since.IsZero() {
		params.Set("startTimestamp", strconv.FormatInt(since.Unix(), 10))
	}

	total := 0
	pages := 0

	for {
		body, err := c.doRequest(ctx, http.MethodGet, reqURL, params)
		if err != nil {
			return fmt.Errorf("GetTransactions failed: %w", err)
		}

		var resp TransactionsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return fmt.Errorf("failed to decode Noves response: %w", err)
		}

		pages++
		total += len(resp.Items)

		if len(resp.Items) > 0 {
			if err := onPage(resp.Items); err != nil {
				return err
			}
		}

		if !resp.HasNextPage || resp.NextPageURL == "" {
			break
		}

		// nextPageUrl is an absolute URL with all params already embedded.
		reqURL = resp.NextPageURL
		params = nil
	}

	c.logger.Info("transactions fetched", "chain", chain, "address", address,
		"count", total, "pages", pages, "duration_ms", time.Since(fetchStart).Milliseconds())
	return nil
}

// GetBalances fetches the current token balances for a single chain and address.
// The chain is a Noves short slug (e.g. "eth", "base"). The endpoint returns a
// top-level JSON array of balance items; for degenerate wallets it instead
// returns a `{detail: ...}` error object (e.g. "too many ERC20 token balances"),
// which we surface as an error rather than silently returning no balances.
func (c *Client) GetBalances(ctx context.Context, chain, address string) ([]BalanceItem, error) {
	fetchStart := time.Now()
	reqURL := fmt.Sprintf("%s/evm/%s/tokens/balancesOf/%s", c.baseURL, chain, address)

	body, err := c.doRequest(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GetBalances failed: %w", err)
	}

	var items []BalanceItem
	if err := json.Unmarshal(body, &items); err != nil {
		// The endpoint returns a `{detail: ...}` object instead of an array for
		// degenerate wallets. Detect that shape and surface it as an error.
		var envelope balancesErrorEnvelope
		if jerr := json.Unmarshal(body, &envelope); jerr == nil && envelope.Detail != "" {
			return nil, fmt.Errorf("Noves balances error for %s on %s: %s", address, chain, envelope.Detail)
		}
		return nil, fmt.Errorf("failed to decode Noves balances response: %w", err)
	}

	c.logger.Info("balances fetched", "chain", chain, "address", address, "count", len(items), "duration_ms", time.Since(fetchStart).Milliseconds())
	return items, nil
}

// RateLimitError represents a rate limit error from the Noves API.
type RateLimitError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s (retry after %s)", e.Message, e.RetryAfter)
}

// IsRateLimitError reports whether an error is (or wraps) a Noves rate limit error.
func IsRateLimitError(err error) bool {
	var rle *RateLimitError
	return errors.As(err, &rle)
}
