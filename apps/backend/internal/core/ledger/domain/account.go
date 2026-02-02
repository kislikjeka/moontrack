package domain

import (
	"time"

	"github.com/google/uuid"
)

// AccountType represents the type of ledger account
type AccountType string

const (
	AccountTypeCryptoWallet AccountType = "CRYPTO_WALLET"
	AccountTypeIncome       AccountType = "INCOME"
	AccountTypeExpense      AccountType = "EXPENSE"
	AccountTypeGasFee       AccountType = "GAS_FEE"
)

// Account represents a ledger account for tracking balances
type Account struct {
	ID        uuid.UUID
	Code      string      // Human-readable code (e.g., "wallet.{wallet_id}.{asset_id}")
	Type      AccountType
	AssetID   string // BTC, ETH, USDC, etc.
	WalletID  *uuid.UUID // Optional: NULL for INCOME/EXPENSE/GAS_FEE accounts
	ChainID   *string    // Optional: inherited from wallet or standalone
	CreatedAt time.Time
	Metadata  map[string]interface{} // Extensible metadata (stored as JSONB)
}

// IsWalletAccount returns true if this is a crypto wallet account
func (a *Account) IsWalletAccount() bool {
	return a.Type == AccountTypeCryptoWallet
}

// IsIncomeAccount returns true if this is an income account
func (a *Account) IsIncomeAccount() bool {
	return a.Type == AccountTypeIncome
}

// IsExpenseAccount returns true if this is an expense account
func (a *Account) IsExpenseAccount() bool {
	return a.Type == AccountTypeExpense
}

// IsGasFeeAccount returns true if this is a gas fee account
func (a *Account) IsGasFeeAccount() bool {
	return a.Type == AccountTypeGasFee
}

// Validate validates the account
func (a *Account) Validate() error {
	if a.Code == "" {
		return ErrInvalidAccountCode
	}

	if a.AssetID == "" {
		return ErrInvalidAssetID
	}

	// Validate account type
	switch a.Type {
	case AccountTypeCryptoWallet:
		if a.WalletID == nil {
			return ErrWalletAccountRequiresWalletID
		}
	case AccountTypeIncome, AccountTypeExpense, AccountTypeGasFee:
		// These accounts should not have a wallet ID
		if a.WalletID != nil {
			return ErrNonWalletAccountCannotHaveWalletID
		}
	default:
		return ErrInvalidAccountType
	}

	return nil
}
