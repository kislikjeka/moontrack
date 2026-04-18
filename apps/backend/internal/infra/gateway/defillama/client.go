// apps/backend/internal/infra/gateway/defillama/client.go
package defillama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/price"
)

const priceScale = 8

// maxResponseBytes bounds the size of a 3rd-party JSON response we will decode.
// Protects against hostile or MITM'd providers returning arbitrarily large payloads
// (OOM / DoS vector). 1 MiB is well above legitimate response sizes for this API.
const maxResponseBytes = 1 << 20 // 1 MiB

// minConfidenceFloor is the lowest MinConfidence value we will honor from
// configuration. Below this floor, we override to the safe default.
//
// Rationale: DefiLlama's confidence field reflects oracle liquidity / freshness.
// A deployment-time misconfiguration such as DEFILLAMA_MIN_CONFIDENCE=0.01
// effectively disables the gate and lets low-quality prices enter the ledger.
// We enforce a hard lower bound at the client level so no runtime configuration
// can silently bypass the gate.
const minConfidenceFloor = 0.5

// safeMinConfidenceDefault is what we reset to if the caller's value is below
// the floor.
const safeMinConfidenceDefault = 0.9

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
		mc = safeMinConfidenceDefault
	}
	// Enforce a hard floor: below minConfidenceFloor we refuse the operator's
	// value and fall back to the safe default. This guards against
	// misconfigurations like DEFILLAMA_MIN_CONFIDENCE=0.01 that would silently
	// let low-quality prices enter the ledger. Logged at WARN so the override
	// is discoverable in ops logs.
	if mc < minConfidenceFloor {
		log.Printf("WARN defillama: configured MinConfidence=%.3f is below floor %.2f; overriding to %.2f",
			mc, minConfidenceFloor, safeMinConfidenceDefault)
		mc = safeMinConfidenceDefault
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
		return nil, &price.RateLimitedError{RetryAfter: price.ParseRetryAfter(resp.Header.Get("Retry-After"))}
	case resp.StatusCode == http.StatusNotFound:
		return nil, price.ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		// Auth failures require operator intervention; treat as transient so we
		// don't burn attempts while a human fixes the API key.
		return nil, fmt.Errorf("%w: auth failure %d", price.ErrTransient, resp.StatusCode)
	case resp.StatusCode >= 500:
		return nil, price.ErrTransient
	case resp.StatusCode >= 400:
		// Unknown 4xx — don't assume it's NotFound; treat as transient.
		return nil, fmt.Errorf("%w: unexpected 4xx %d", price.ErrTransient, resp.StatusCode)
	}

	var out coinsResponse
	bodyReader := io.LimitReader(resp.Body, maxResponseBytes)
	if err := json.NewDecoder(bodyReader).Decode(&out); err != nil {
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
