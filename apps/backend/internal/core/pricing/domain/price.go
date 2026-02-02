package domain

import (
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// PriceSource represents the source of a price
type PriceSource string

const (
	PriceSourceCoinGecko PriceSource = "coingecko"
	PriceSourceManual    PriceSource = "manual"
)

// PriceSnapshot represents a historical USD price for an asset
type PriceSnapshot struct {
	ID           uuid.UUID
	AssetID      string      // BTC, ETH, USDC, etc.
	USDPrice     *big.Int    // Price scaled by 10^8 (e.g., $45678.90 → 4567890000000)
	Source       PriceSource
	SnapshotDate time.Time // Date of price (date component only)
	CreatedAt    time.Time
}

// Validate validates the price snapshot
func (p *PriceSnapshot) Validate() error {
	if p.AssetID == "" {
		return ErrInvalidAssetID
	}

	if p.USDPrice == nil {
		return ErrNilUSDPrice
	}

	if p.USDPrice.Sign() < 0 {
		return ErrNegativeUSDPrice
	}

	if p.Source != PriceSourceCoinGecko && p.Source != PriceSourceManual {
		return ErrInvalidPriceSource
	}

	if p.SnapshotDate.IsZero() {
		return ErrInvalidSnapshotDate
	}

	return nil
}

// ToDate returns the snapshot date normalized to midnight UTC
func (p *PriceSnapshot) ToDate() time.Time {
	return time.Date(
		p.SnapshotDate.Year(),
		p.SnapshotDate.Month(),
		p.SnapshotDate.Day(),
		0, 0, 0, 0,
		time.UTC,
	)
}

// PriceSnapshot errors
var (
	ErrInvalidAssetID      = errors.New("invalid asset ID")
	ErrNilUSDPrice         = errors.New("USD price cannot be nil")
	ErrNegativeUSDPrice    = errors.New("USD price cannot be negative")
	ErrInvalidPriceSource  = errors.New("invalid price source")
	ErrInvalidSnapshotDate = errors.New("invalid snapshot date")
	ErrPriceNotFound       = errors.New("price not found")
	ErrPriceAPIUnavailable = errors.New("price API unavailable")
)
