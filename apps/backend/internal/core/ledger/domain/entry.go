package domain

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// DebitCredit represents whether an entry is a debit or credit
type DebitCredit string

const (
	Debit  DebitCredit = "DEBIT"
	Credit DebitCredit = "CREDIT"
)

// EntryType represents the type of ledger entry
type EntryType string

const (
	EntryTypeAssetIncrease EntryType = "asset_increase"
	EntryTypeAssetDecrease EntryType = "asset_decrease"
	EntryTypeIncome        EntryType = "income"
	EntryTypeExpense       EntryType = "expense"
	EntryTypeGasFee        EntryType = "gas_fee"
)

// Entry represents a single debit or credit entry in the double-entry ledger
// IMMUTABLE: Entries are never updated or deleted per constitution
type Entry struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	AccountID     uuid.UUID
	DebitCredit   DebitCredit
	EntryType     EntryType
	Amount        *big.Int // Amount in base units (wei, satoshi, lamports)
	AssetID       string
	USDRate       *big.Int  // USD rate scaled by 10^8
	USDValue      *big.Int  // amount * usd_rate / 10^8
	OccurredAt    time.Time // Matches transaction occurred_at
	CreatedAt     time.Time
	Metadata      map[string]interface{} // Extensible metadata (stored as JSONB)
}

// Validate validates the entry
func (e *Entry) Validate() error {
	// Validate debit/credit
	if e.DebitCredit != Debit && e.DebitCredit != Credit {
		return ErrInvalidDebitCredit
	}

	// Validate amounts are non-negative
	if e.Amount == nil || e.Amount.Sign() < 0 {
		return ErrNegativeAmount
	}

	if e.USDRate == nil || e.USDRate.Sign() < 0 {
		return ErrNegativeUSDRate
	}

	if e.USDValue == nil || e.USDValue.Sign() < 0 {
		return ErrNegativeUSDValue
	}

	// Validate asset ID
	if e.AssetID == "" {
		return ErrInvalidAssetID
	}

	return nil
}

// IsDebit returns true if this entry is a debit
func (e *Entry) IsDebit() bool {
	return e.DebitCredit == Debit
}

// IsCredit returns true if this entry is a credit
func (e *Entry) IsCredit() bool {
	return e.DebitCredit == Credit
}

// SignedAmount returns the amount with the appropriate sign for balance calculations
// Debits are positive, credits are negative
func (e *Entry) SignedAmount() *big.Int {
	result := new(big.Int).Set(e.Amount)
	if e.IsCredit() {
		result.Neg(result)
	}
	return result
}
