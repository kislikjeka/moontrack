package rawdata

import (
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// OutcomeData represents parsed data from a manual outcome transaction's raw_data
type OutcomeData struct {
	WalletID     uuid.UUID
	AssetID      string
	PriceAssetID string
	Decimals     int
	Amount       *money.BigInt
	USDRate      *money.BigInt
	OccurredAt   time.Time
	Notes        string
	PriceSource  string
}

// GetAmount returns the amount as *big.Int
func (d *OutcomeData) GetAmount() *big.Int {
	return d.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (d *OutcomeData) GetUSDRate() *big.Int {
	return d.USDRate.ToBigInt()
}

// ParseOutcomeFromRawData parses a raw_data map into OutcomeData
func ParseOutcomeFromRawData(raw map[string]interface{}) (*OutcomeData, error) {
	data := &OutcomeData{}

	// Parse wallet_id
	if walletIDStr, ok := raw["wallet_id"].(string); ok {
		walletID, err := uuid.Parse(walletIDStr)
		if err != nil {
			return nil, ErrInvalidWalletID
		}
		data.WalletID = walletID
	}

	// Parse asset_id
	if assetID, ok := raw["asset_id"].(string); ok {
		data.AssetID = assetID
	}

	// Parse price_asset_id
	if priceAssetID, ok := raw["price_asset_id"].(string); ok {
		data.PriceAssetID = priceAssetID
	}

	// Parse amount
	if amountStr, ok := raw["amount"].(string); ok {
		amount, ok := money.NewBigIntFromString(amountStr)
		if !ok {
			return nil, ErrInvalidAmount
		}
		data.Amount = amount
	}

	// Parse usd_rate (optional)
	if usdRateStr, ok := raw["usd_rate"].(string); ok && usdRateStr != "" {
		usdRate, ok := money.NewBigIntFromString(usdRateStr)
		if !ok {
			return nil, ErrInvalidUSDRate
		}
		data.USDRate = usdRate
	}

	// Parse occurred_at
	if occurredAtStr, ok := raw["occurred_at"].(string); ok {
		occurredAt, err := time.Parse(time.RFC3339, occurredAtStr)
		if err != nil {
			return nil, err
		}
		data.OccurredAt = occurredAt
	}

	// Parse notes (optional)
	if notes, ok := raw["notes"].(string); ok {
		data.Notes = notes
	}

	// Parse price_source (optional)
	if priceSource, ok := raw["price_source"].(string); ok {
		data.PriceSource = priceSource
	}

	// Parse decimals (default to 8 if not provided)
	data.Decimals = 8 // default
	if decimals, ok := raw["decimals"].(float64); ok {
		data.Decimals = int(decimals)
	} else if decimals, ok := raw["decimals"].(int); ok {
		data.Decimals = decimals
	}

	return data, nil
}
