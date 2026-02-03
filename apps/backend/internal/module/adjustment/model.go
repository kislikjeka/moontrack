package adjustment

import (
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// AssetAdjustmentTransaction represents a manual asset balance adjustment
type AssetAdjustmentTransaction struct {
	WalletID    uuid.UUID     `json:"wallet_id"`
	AssetID     string        `json:"asset_id"`
	Decimals    int           `json:"decimals"`           // Asset decimals for USD value calculation
	NewBalance  *money.BigInt `json:"new_balance"`        // Target balance in base units
	USDRate     *money.BigInt `json:"usd_rate,omitempty"` // Optional: USD rate scaled by 10^8
	OccurredAt  time.Time     `json:"occurred_at"`
	Notes       string        `json:"notes,omitempty"`        // Reason for adjustment
	PriceSource string        `json:"price_source,omitempty"` // "manual" or "coingecko"
}

// Validate validates the asset adjustment transaction
func (t *AssetAdjustmentTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}

	if t.AssetID == "" {
		return ErrMissingAssetID
	}

	if t.NewBalance.IsNil() {
		return ErrMissingNewBalance
	}

	if t.NewBalance.Sign() < 0 {
		return ErrNegativeBalance
	}

	if !t.USDRate.IsNil() && t.USDRate.Sign() < 0 {
		return ErrNegativeUSDRate
	}

	if t.OccurredAt.After(time.Now()) {
		return ErrFutureDate
	}

	return nil
}

// GetNewBalance returns the new balance as *big.Int
func (t *AssetAdjustmentTransaction) GetNewBalance() *big.Int {
	return t.NewBalance.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (t *AssetAdjustmentTransaction) GetUSDRate() *big.Int {
	return t.USDRate.ToBigInt()
}

// ParseAdjustmentFromRawData parses a raw_data map into AssetAdjustmentTransaction
func ParseAdjustmentFromRawData(raw map[string]interface{}) (*AssetAdjustmentTransaction, error) {
	tx := &AssetAdjustmentTransaction{}

	// Parse wallet_id
	if walletIDStr, ok := raw["wallet_id"].(string); ok {
		walletID, err := uuid.Parse(walletIDStr)
		if err != nil {
			return nil, ErrInvalidWalletID
		}
		tx.WalletID = walletID
	}

	// Parse asset_id
	if assetID, ok := raw["asset_id"].(string); ok {
		tx.AssetID = assetID
	}

	// Parse new_balance
	if newBalanceStr, ok := raw["new_balance"].(string); ok {
		newBalance, ok := money.NewBigIntFromString(newBalanceStr)
		if !ok {
			return nil, ErrMissingNewBalance
		}
		tx.NewBalance = newBalance
	}

	// Parse usd_rate (optional)
	if usdRateStr, ok := raw["usd_rate"].(string); ok && usdRateStr != "" {
		usdRate, ok := money.NewBigIntFromString(usdRateStr)
		if !ok {
			return nil, ErrNegativeUSDRate
		}
		tx.USDRate = usdRate
	}

	// Parse occurred_at
	if occurredAtStr, ok := raw["occurred_at"].(string); ok {
		occurredAt, err := time.Parse(time.RFC3339, occurredAtStr)
		if err != nil {
			return nil, err
		}
		tx.OccurredAt = occurredAt
	}

	// Parse notes (optional)
	if notes, ok := raw["notes"].(string); ok {
		tx.Notes = notes
	}

	// Parse price_source (optional)
	if priceSource, ok := raw["price_source"].(string); ok {
		tx.PriceSource = priceSource
	}

	// Parse decimals (default to 8 if not provided)
	tx.Decimals = 8 // default
	if decimals, ok := raw["decimals"].(float64); ok {
		tx.Decimals = int(decimals)
	} else if decimals, ok := raw["decimals"].(int); ok {
		tx.Decimals = decimals
	}

	return tx, nil
}
