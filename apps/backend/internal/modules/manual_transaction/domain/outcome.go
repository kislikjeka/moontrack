package domain

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// ManualOutcomeTransaction represents a manually recorded outgoing asset transaction
// Examples: withdrawals, sales, payments, transfers out
type ManualOutcomeTransaction struct {
	WalletID    uuid.UUID `json:"wallet_id"`
	AssetID     string    `json:"asset_id"`      // BTC, ETH, USDC, etc.
	Amount      *big.Int  `json:"amount"`        // Amount in base units (wei, satoshi, lamports)
	USDRate     *big.Int  `json:"usd_rate"`      // Optional: USD rate scaled by 10^8, if null fetch from CoinGecko
	OccurredAt  time.Time `json:"occurred_at"`
	Notes       string    `json:"notes"`         // Optional: user notes
	PriceSource string    `json:"price_source"`  // Optional: "manual" or "coingecko" - for audit trail
}

// Validate validates the manual outcome transaction
func (t *ManualOutcomeTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}

	if t.AssetID == "" {
		return ErrInvalidAssetID
	}

	if t.Amount == nil || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}

	// If USD rate is provided, it must be positive
	if t.USDRate != nil && t.USDRate.Sign() <= 0 {
		return ErrInvalidUSDRate
	}

	// Occurred at cannot be in the future
	if t.OccurredAt.After(time.Now()) {
		return ErrOccurredAtInFuture
	}

	return nil
}

// HasManualPrice returns true if the transaction has a manually specified USD rate
func (t *ManualOutcomeTransaction) HasManualPrice() bool {
	return t.USDRate != nil && t.USDRate.Sign() > 0
}
