package domain

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// AccountBalance represents the denormalized current balance for an account/asset
// This is maintained for performance and is reconcilable from ledger entries
type AccountBalance struct {
	AccountID   uuid.UUID
	AssetID     string
	Balance     *big.Int  // Current balance in base units
	USDValue    *big.Int  // At current price, updated periodically
	LastUpdated time.Time
}

// Validate validates the account balance
func (b *AccountBalance) Validate() error {
	if b.AssetID == "" {
		return ErrInvalidAssetID
	}

	if b.Balance == nil || b.Balance.Sign() < 0 {
		return ErrNegativeBalance
	}

	if b.USDValue == nil || b.USDValue.Sign() < 0 {
		return ErrNegativeUSDValue
	}

	return nil
}

// IsZero returns true if the balance is zero
func (b *AccountBalance) IsZero() bool {
	return b.Balance.Sign() == 0
}

// IsPositive returns true if the balance is positive
func (b *AccountBalance) IsPositive() bool {
	return b.Balance.Sign() > 0
}
