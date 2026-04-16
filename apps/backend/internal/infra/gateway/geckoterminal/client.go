// apps/backend/internal/infra/gateway/geckoterminal/client.go
package geckoterminal

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

// priceScale — USD prices are stored as big.Int scaled 10^8 throughout the codebase.
const priceScale = 8

// Config for the GeckoTerminal client.
type Config struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
}

type Client struct {
	baseURL string
	http    *http.Client
}

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
		base = "https://api.geckoterminal.com/api/v2"
	}
	return &Client{baseURL: base, http: hc}
}

type tokenMultiResponse struct {
	Data []struct {
		Attributes struct {
			Address  string `json:"address"`
			PriceUSD string `json:"price_usd"`
		} `json:"attributes"`
	} `json:"data"`
}

func decimalToBigIntScaled(s string) (*big.Int, error) {
	if s == "" {
		return nil, fmt.Errorf("empty decimal")
	}
	f, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("bad decimal %q", s)
	}
	mult := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(priceScale)), nil)
	scaled := new(big.Rat).Mul(f, new(big.Rat).SetInt(mult))
	out := new(big.Int).Div(scaled.Num(), scaled.Denom())
	return out, nil
}

// GetTokenPriceByAddress calls /networks/{network}/tokens/multi/{address}.
// Returns a big.Int scaled by 10^8.
func (c *Client) GetTokenPriceByAddress(ctx context.Context, network, address string) (*big.Int, error) {
	u := fmt.Sprintf("%s/networks/%s/tokens/multi/%s",
		c.baseURL, url.PathEscape(network), url.PathEscape(address))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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

	var out tokenMultiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", price.ErrTransient, err)
	}
	if len(out.Data) == 0 || out.Data[0].Attributes.PriceUSD == "" {
		return nil, price.ErrNotFound
	}
	return decimalToBigIntScaled(out.Data[0].Attributes.PriceUSD)
}

type ohlcvResponse struct {
	Data struct {
		Attributes struct {
			List [][]interface{} `json:"ohlcv_list"`
		} `json:"attributes"`
	} `json:"data"`
}

// GetPoolOHLCVMinute returns the minute candle close price nearest to `at`.
func (c *Client) GetPoolOHLCVMinute(ctx context.Context, network, poolAddress string, at time.Time) (*price.HistoricalPrice, error) {
	u := fmt.Sprintf("%s/networks/%s/pools/%s/ohlcv/minute?before_timestamp=%d&limit=5",
		c.baseURL, url.PathEscape(network), url.PathEscape(poolAddress), at.Unix()+60)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
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

	var out ohlcvResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", price.ErrTransient, err)
	}
	if len(out.Data.Attributes.List) == 0 {
		return nil, price.ErrNotFound
	}

	// Each entry: [unix, open, high, low, close, volume]
	// Pick the candle whose timestamp is closest to `at`.
	var best []interface{}
	var bestDelta int64 = 1 << 62
	for _, cnd := range out.Data.Attributes.List {
		if len(cnd) < 5 {
			continue
		}
		var tsInt int64
		switch v := cnd[0].(type) {
		case float64:
			tsInt = int64(v)
		case int64:
			tsInt = v
		default:
			continue
		}
		delta := tsInt - at.Unix()
		if delta < 0 {
			delta = -delta
		}
		if delta < bestDelta {
			bestDelta = delta
			best = cnd
		}
	}
	if best == nil {
		return nil, price.ErrNotFound
	}
	closeStr, _ := best[4].(string)
	if closeStr == "" {
		// sometimes numeric
		if f, ok := best[4].(float64); ok {
			closeStr = strconv.FormatFloat(f, 'f', -1, 64)
		}
	}
	priceBI, err := decimalToBigIntScaled(closeStr)
	if err != nil {
		return nil, fmt.Errorf("%w: bad close: %v", price.ErrTransient, err)
	}
	var ts int64
	if f, ok := best[0].(float64); ok {
		ts = int64(f)
	}
	return &price.HistoricalPrice{
		PriceUSD:   priceBI,
		Timestamp:  time.Unix(ts, 0).UTC(),
		Confidence: 1,
	}, nil
}
