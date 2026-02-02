package domain

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// AssetAdjustmentTransaction represents a manual asset balance adjustment
type AssetAdjustmentTransaction struct {
	WalletID   uuid.UUID `json:"wallet_id"`
	AssetID    string    `json:"asset_id"`
	NewBalance *big.Int  `json:"new_balance"` // Target balance in base units
	USDRate    *big.Int  `json:"usd_rate,omitempty"` // Optional: USD rate scaled by 10^8
	OccurredAt time.Time `json:"occurred_at"`
	Notes      string    `json:"notes,omitempty"` // Reason for adjustment
	PriceSource string   `json:"price_source,omitempty"` // "manual" or "coingecko"
}

// Validate validates the asset adjustment transaction
func (t *AssetAdjustmentTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}

	if t.AssetID == "" {
		return ErrMissingAssetID
	}

	if t.NewBalance == nil {
		return ErrMissingNewBalance
	}

	if t.NewBalance.Sign() < 0 {
		return ErrNegativeBalance
	}

	if t.USDRate != nil && t.USDRate.Sign() < 0 {
		return ErrNegativeUSDRate
	}

	if t.OccurredAt.After(time.Now()) {
		return ErrFutureDate
	}

	return nil
}
