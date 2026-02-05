package rawdata

import (
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// AdjustmentData represents parsed data from an asset adjustment transaction's raw_data
type AdjustmentData struct {
	WalletID    uuid.UUID
	AssetID     string
	Decimals    int
	NewBalance  *money.BigInt
	USDRate     *money.BigInt
	OccurredAt  time.Time
	Notes       string
	PriceSource string
}

// GetNewBalance returns the new balance as *big.Int
func (d *AdjustmentData) GetNewBalance() *big.Int {
	return d.NewBalance.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (d *AdjustmentData) GetUSDRate() *big.Int {
	return d.USDRate.ToBigInt()
}

// ParseAdjustmentFromRawData parses a raw_data map into AdjustmentData
func ParseAdjustmentFromRawData(raw map[string]interface{}) (*AdjustmentData, error) {
	data := &AdjustmentData{}

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

	// Parse new_balance
	if newBalanceStr, ok := raw["new_balance"].(string); ok {
		newBalance, ok := money.NewBigIntFromString(newBalanceStr)
		if !ok {
			return nil, ErrMissingNewBalance
		}
		data.NewBalance = newBalance
	}

	// Parse usd_rate (optional)
	if usdRateStr, ok := raw["usd_rate"].(string); ok && usdRateStr != "" {
		usdRate, ok := money.NewBigIntFromString(usdRateStr)
		if !ok {
			return nil, ErrNegativeUSDRate
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
