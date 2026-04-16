// apps/backend/internal/infra/gateway/defillama/client.go
package defillama

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/price"
)

const priceScale = 8

// Config for the DefiLlama Coins API client.
type Config struct {
	BaseURL       string
	HTTPClient    *http.Client
	Timeout       time.Duration
	MinConfidence float64
}

// Client is a DefiLlama Coins API gateway client.
type Client struct {
	baseURL       string
	http          *http.Client
	minConfidence float64
}

// NewClient constructs a Client. Defaults: BaseURL = https://coins.llama.fi, MinConfidence = 0.9.
func NewClient(cfg Config) *Client {
	hc := cfg.HTTPClient
	if hc == nil {
		to := cfg.Timeout
		if to == 0 {
			to = 10 * time.Second
		}
		hc = &http.Client{Timeout: to}
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://coins.llama.fi"
	}
	mc := cfg.MinConfidence
	if mc == 0 {
		mc = 0.9
	}
	return &Client{baseURL: base, http: hc, minConfidence: mc}
}

type coinEntry struct {
	Price      float64 `json:"price"`
	Timestamp  int64   `json:"timestamp"`
	Confidence float64 `json:"confidence"`
}

type coinsResponse struct {
	Coins map[string]coinEntry `json:"coins"`
}

// floatToBigIntScaled converts a float64 price to big.Int scaled by 10^8 using
// rational arithmetic to avoid floating-point loss.
func floatToBigIntScaled(f float64) *big.Int {
	// Format with fixed precision to avoid scientific notation, then reparse as rational.
	s := strconv.FormatFloat(f, 'f', -1, 64)
	rat, _ := new(big.Rat).SetString(s)
	if rat == nil {
		return big.NewInt(0)
	}
	mult := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(priceScale)), nil)
	scaled := new(big.Rat).Mul(rat, new(big.Rat).SetInt(mult))
	return new(big.Int).Div(scaled.Num(), scaled.Denom())
}

// doCoins issues a GET to path and decodes the coins response.
// Maps 429 → ErrRateLimited, 404 → ErrNotFound, 5xx → ErrTransient.
func (c *Client) doCoins(ctx context.Context, path string) (*coinsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", price.ErrTransient, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, price.ErrRateLimited
	case resp.StatusCode == http.StatusNotFound:
		return nil, price.ErrNotFound
	case resp.StatusCode >= 500:
		return nil, price.ErrTransient
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("%w: status %d", price.ErrNotFound, resp.StatusCode)
	}

	var out coinsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", price.ErrTransient, err)
	}
	return &out, nil
}

// GetCurrentPrice hits /prices/current/{chain:addr} and returns scaled big.Int (10^8).
func (c *Client) GetCurrentPrice(ctx context.Context, chain, addr string) (*big.Int, error) {
	key := fmt.Sprintf("%s:%s", chain, addr)
	out, err := c.doCoins(ctx, "/prices/current/"+url.PathEscape(key))
	if err != nil {
		return nil, err
	}
	entry, ok := out.Coins[key]
	if !ok {
		return nil, price.ErrNotFound
	}
	if entry.Confidence < c.minConfidence {
		return nil, price.ErrLowConfidence
	}
	return floatToBigIntScaled(entry.Price), nil
}

// GetHistoricalPrice hits /prices/historical/{unix}/{chain:addr}.
func (c *Client) GetHistoricalPrice(ctx context.Context, chain, addr string, at time.Time) (*price.HistoricalPrice, error) {
	key := fmt.Sprintf("%s:%s", chain, addr)
	out, err := c.doCoins(ctx, fmt.Sprintf("/prices/historical/%d/%s", at.Unix(), url.PathEscape(key)))
	if err != nil {
		return nil, err
	}
	entry, ok := out.Coins[key]
	if !ok {
		return nil, price.ErrNotFound
	}
	if entry.Confidence < c.minConfidence {
		return nil, price.ErrLowConfidence
	}
	return &price.HistoricalPrice{
		PriceUSD:   floatToBigIntScaled(entry.Price),
		Timestamp:  time.Unix(entry.Timestamp, 0).UTC(),
		Confidence: entry.Confidence,
	}, nil
}
