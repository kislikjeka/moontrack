package asset

import (
	"encoding/json"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// AssetType represents the type of asset
type AssetType string

const (
	AssetTypeCrypto AssetType = "crypto"
	AssetTypeFiat   AssetType = "fiat"
	AssetTypeCustom AssetType = "custom"
)

// Asset represents a cryptocurrency or token in the registry
type Asset struct {
	ID              uuid.UUID
	Symbol          string // BTC, ETH, USDC
	Name            string // Bitcoin, Ethereum, USD Coin
	CoinGeckoID     string // bitcoin, ethereum, usd-coin
	Decimals        int    // 8 for BTC, 18 for ETH, 6 for USDC
	AssetType       AssetType
	ChainID         *string // ethereum, solana, polygon (nil for native L1)
	ContractAddress *string // 0xA0b86991c6218... (nil for native)
	MarketCapRank   *int
	IsActive        bool
	Metadata        json.RawMessage
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Validate validates the asset fields
func (a *Asset) Validate() error {
	if a.Symbol == "" {
		return ErrInvalidSymbol
	}

	if a.Name == "" {
		return ErrInvalidName
	}

	if a.CoinGeckoID == "" {
		return ErrCoinGeckoIDRequired
	}

	if a.Decimals < 0 || a.Decimals > 78 {
		return ErrInvalidDecimals
	}

	if a.AssetType != AssetTypeCrypto && a.AssetType != AssetTypeFiat && a.AssetType != AssetTypeCustom {
		return ErrInvalidAssetType
	}

	return nil
}

// IsNativeL1 returns true if the asset is a native L1 coin (not a token on another chain)
func (a *Asset) IsNativeL1() bool {
	return a.ChainID == nil
}

// IsToken returns true if the asset is a token on a chain
func (a *Asset) IsToken() bool {
	return a.ChainID != nil
}

// ChainLabel returns a display label for the chain (empty if native L1)
func (a *Asset) ChainLabel() string {
	if a.ChainID == nil {
		return ""
	}
	return *a.ChainID
}

// NewAsset creates a new Asset with the given parameters
func NewAsset(symbol, name, coinGeckoID string, decimals int) *Asset {
	return &Asset{
		ID:          uuid.New(),
		Symbol:      symbol,
		Name:        name,
		CoinGeckoID: coinGeckoID,
		Decimals:    decimals,
		AssetType:   AssetTypeCrypto,
		IsActive:    true,
		Metadata:    json.RawMessage("{}"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// WithChain sets the chain information for a token asset
func (a *Asset) WithChain(chainID, contractAddress string) *Asset {
	a.ChainID = &chainID
	a.ContractAddress = &contractAddress
	return a
}

// WithMarketCapRank sets the market cap rank
func (a *Asset) WithMarketCapRank(rank int) *Asset {
	a.MarketCapRank = &rank
	return a
}

// PriceSource represents the source of a price
type PriceSource string

const (
	PriceSourceCoinGecko PriceSource = "coingecko"
	PriceSourceManual    PriceSource = "manual"
)

// PriceInterval represents the granularity of price data
type PriceInterval string

const (
	PriceIntervalHourly PriceInterval = "1h"
	PriceIntervalDaily  PriceInterval = "1d"
	PriceIntervalWeekly PriceInterval = "1w"
)

// PricePoint represents a single price record
type PricePoint struct {
	Time      time.Time
	AssetID   uuid.UUID
	PriceUSD  *big.Int // scaled by 10^8 (e.g., $45678.90 → 4567890000000)
	Volume24h *big.Int // optional, scaled by 10^8
	MarketCap *big.Int // optional, scaled by 10^8
	Source    PriceSource
	IsStale   bool // true if this price came from stale cache
}

// Validate validates the price point
func (p *PricePoint) Validate() error {
	if p.AssetID == uuid.Nil {
		return ErrAssetNotFound
	}

	if p.PriceUSD == nil {
		return ErrNilPrice
	}

	if p.PriceUSD.Sign() < 0 {
		return ErrNegativePrice
	}

	return nil
}

// OHLCV represents Open/High/Low/Close/Volume data for a period
type OHLCV struct {
	AssetID   uuid.UUID
	Time      time.Time // start of the period (day/week)
	Open      *big.Int  // first price in period
	High      *big.Int  // highest price in period
	Low       *big.Int  // lowest price in period
	Close     *big.Int  // last price in period
	AvgVolume *big.Int  // average volume over period
}

// PriceHistoryQuery defines parameters for querying price history
type PriceHistoryQuery struct {
	AssetID  uuid.UUID
	From     time.Time
	To       time.Time
	Interval PriceInterval
}

// Validate validates the query parameters
func (q *PriceHistoryQuery) Validate() error {
	if q.AssetID == uuid.Nil {
		return ErrAssetNotFound
	}

	if q.From.IsZero() || q.To.IsZero() {
		return ErrInvalidTimeRange
	}

	if q.From.After(q.To) {
		return ErrInvalidTimeRange
	}

	// Max 1 year range
	maxRange := time.Hour * 24 * 365
	if q.To.Sub(q.From) > maxRange {
		return ErrTimeRangeTooLarge
	}

	if q.Interval != PriceIntervalHourly && q.Interval != PriceIntervalDaily && q.Interval != PriceIntervalWeekly {
		return ErrInvalidInterval
	}

	return nil
}

// ParsePriceInterval parses a string into a PriceInterval
func ParsePriceInterval(s string) (PriceInterval, error) {
	switch s {
	case "1h":
		return PriceIntervalHourly, nil
	case "1d":
		return PriceIntervalDaily, nil
	case "1w":
		return PriceIntervalWeekly, nil
	default:
		return "", ErrInvalidInterval
	}
}
