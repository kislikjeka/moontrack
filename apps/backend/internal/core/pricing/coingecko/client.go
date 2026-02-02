package coingecko

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	baseURL              = "https://api.coingecko.com/api/v3"
	headerAPIKey         = "x-cg-demo-api-key"
	requestTimeout       = 10 * time.Second
	rateLimitRetryAfter  = 60 * time.Second
)

// Client represents a CoinGecko API client
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new CoinGecko API client
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		baseURL: baseURL,
	}
}

// PriceResponse represents the response from CoinGecko price API
type PriceResponse struct {
	Prices map[string]float64 // asset_id -> USD price
}

// HistoricalPriceResponse represents the response from CoinGecko historical price API
type HistoricalPriceResponse struct {
	ID          string                 `json:"id"`
	Symbol      string                 `json:"symbol"`
	MarketData  MarketData             `json:"market_data"`
}

// MarketData contains the market data including current price
type MarketData struct {
	CurrentPrice map[string]float64 `json:"current_price"`
}

// GetCurrentPrices fetches current USD prices for multiple assets
// assetIDs: coingecko IDs (e.g., "bitcoin", "ethereum", "usd-coin")
func (c *Client) GetCurrentPrices(ctx context.Context, assetIDs []string) (map[string]*big.Int, error) {
	if len(assetIDs) == 0 {
		return make(map[string]*big.Int), nil
	}

	// Build request URL
	params := url.Values{}
	params.Set("ids", strings.Join(assetIDs, ","))
	params.Set("vs_currencies", "usd")
	params.Set("precision", "8") // Request 8 decimal places

	reqURL := fmt.Sprintf("%s/simple/price?%s", c.baseURL, params.Encode())

	// Make HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(headerAPIKey, c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Handle rate limiting
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{
			RetryAfter: rateLimitRetryAfter,
			Message:    "CoinGecko API rate limit exceeded",
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var rawPrices map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&rawPrices); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to scaled big.Int (multiply by 10^8)
	result := make(map[string]*big.Int, len(rawPrices))
	for assetID, currencies := range rawPrices {
		usdPrice, ok := currencies["usd"]
		if !ok {
			continue
		}

		// Convert float to scaled big.Int
		// Multiply by 10^8 to preserve 8 decimal places
		scaledPrice := scaleFloatToBigInt(usdPrice, 8)
		result[assetID] = scaledPrice
	}

	return result, nil
}

// GetHistoricalPrice fetches the USD price for a specific asset on a specific date
// assetID: coingecko ID (e.g., "bitcoin")
// date: the date to fetch price for
func (c *Client) GetHistoricalPrice(ctx context.Context, assetID string, date time.Time) (*big.Int, error) {
	// Format date as DD-MM-YYYY (CoinGecko format)
	dateStr := date.Format("02-01-2006")

	// Build request URL
	reqURL := fmt.Sprintf("%s/coins/%s/history?date=%s&localization=false", c.baseURL, assetID, dateStr)

	// Make HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set(headerAPIKey, c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Handle rate limiting
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{
			RetryAfter: rateLimitRetryAfter,
			Message:    "CoinGecko API rate limit exceeded",
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var historical HistoricalPriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&historical); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract USD price
	usdPrice, ok := historical.MarketData.CurrentPrice["usd"]
	if !ok {
		return nil, fmt.Errorf("USD price not found in response")
	}

	// Convert to scaled big.Int
	scaledPrice := scaleFloatToBigInt(usdPrice, 8)
	return scaledPrice, nil
}

// scaleFloatToBigInt converts a float64 to a big.Int by scaling by 10^decimals
// Example: scaleFloatToBigInt(45678.90, 8) returns 4567890000000
func scaleFloatToBigInt(value float64, decimals int) *big.Int {
	// Create multiplier: 10^decimals
	multiplier := new(big.Float).SetInt(new(big.Int).Exp(
		big.NewInt(10),
		big.NewInt(int64(decimals)),
		nil,
	))

	// Multiply value by multiplier
	scaled := new(big.Float).Mul(big.NewFloat(value), multiplier)

	// Convert to big.Int (truncate decimals)
	result, _ := scaled.Int(nil)
	return result
}

// RateLimitError represents a rate limit error from CoinGecko API
type RateLimitError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s (retry after %s)", e.Message, e.RetryAfter)
}

// IsRateLimitError checks if an error is a rate limit error
func IsRateLimitError(err error) bool {
	_, ok := err.(*RateLimitError)
	return ok
}
