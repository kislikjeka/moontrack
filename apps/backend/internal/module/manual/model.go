package manual

import (
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// ManualIncomeTransaction represents a manually recorded incoming asset transaction
// Examples: deposits, purchases, rewards, airdrops
type ManualIncomeTransaction struct {
	WalletID     uuid.UUID     `json:"wallet_id"`
	AssetID      string        `json:"asset_id"`       // BTC, ETH, USDC, etc.
	PriceAssetID string        `json:"price_asset_id"` // CoinGecko ID for price lookup (e.g., "bitcoin")
	Decimals     int           `json:"decimals"`       // Asset decimals for USD value calculation
	Amount       *money.BigInt `json:"amount"`         // Amount in base units (wei, satoshi, lamports)
	USDRate      *money.BigInt `json:"usd_rate"`       // Optional: USD rate scaled by 10^8, if null fetch from CoinGecko
	OccurredAt   time.Time     `json:"occurred_at"`
	Notes        string        `json:"notes"`        // Optional: user notes
	PriceSource  string        `json:"price_source"` // Optional: "manual" or "coingecko" - for audit trail
}

// Validate validates the manual income transaction
func (t *ManualIncomeTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}

	if t.AssetID == "" {
		return ErrInvalidAssetID
	}

	if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}

	// If USD rate is provided, it must be positive
	if !t.USDRate.IsNil() && t.USDRate.Sign() <= 0 {
		return ErrInvalidUSDRate
	}

	// Occurred at cannot be in the future
	if t.OccurredAt.After(time.Now()) {
		return ErrOccurredAtInFuture
	}

	return nil
}

// GetAmount returns the amount as *big.Int
func (t *ManualIncomeTransaction) GetAmount() *big.Int {
	return t.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (t *ManualIncomeTransaction) GetUSDRate() *big.Int {
	return t.USDRate.ToBigInt()
}

// HasManualPrice returns true if the transaction has a manually specified USD rate
func (t *ManualIncomeTransaction) HasManualPrice() bool {
	return t.USDRate != nil && t.USDRate.Sign() > 0
}

// ParseIncomeFromRawData parses a raw_data map into ManualIncomeTransaction
func ParseIncomeFromRawData(raw map[string]interface{}) (*ManualIncomeTransaction, error) {
	tx := &ManualIncomeTransaction{}

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

	// Parse price_asset_id
	if priceAssetID, ok := raw["price_asset_id"].(string); ok {
		tx.PriceAssetID = priceAssetID
	}

	// Parse amount
	if amountStr, ok := raw["amount"].(string); ok {
		amount, ok := money.NewBigIntFromString(amountStr)
		if !ok {
			return nil, ErrInvalidAmount
		}
		tx.Amount = amount
	}

	// Parse usd_rate (optional)
	if usdRateStr, ok := raw["usd_rate"].(string); ok && usdRateStr != "" {
		usdRate, ok := money.NewBigIntFromString(usdRateStr)
		if !ok {
			return nil, ErrInvalidUSDRate
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

// ManualOutcomeTransaction represents a manually recorded outgoing asset transaction
// Examples: withdrawals, sales, payments, transfers out
type ManualOutcomeTransaction struct {
	WalletID     uuid.UUID     `json:"wallet_id"`
	AssetID      string        `json:"asset_id"`       // BTC, ETH, USDC, etc.
	PriceAssetID string        `json:"price_asset_id"` // CoinGecko ID for price lookup (e.g., "bitcoin")
	Decimals     int           `json:"decimals"`       // Asset decimals for USD value calculation
	Amount       *money.BigInt `json:"amount"`         // Amount in base units (wei, satoshi, lamports)
	USDRate      *money.BigInt `json:"usd_rate"`       // Optional: USD rate scaled by 10^8, if null fetch from CoinGecko
	OccurredAt   time.Time     `json:"occurred_at"`
	Notes        string        `json:"notes"`        // Optional: user notes
	PriceSource  string        `json:"price_source"` // Optional: "manual" or "coingecko" - for audit trail
}

// Validate validates the manual outcome transaction
func (t *ManualOutcomeTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}

	if t.AssetID == "" {
		return ErrInvalidAssetID
	}

	if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}

	// If USD rate is provided, it must be positive
	if !t.USDRate.IsNil() && t.USDRate.Sign() <= 0 {
		return ErrInvalidUSDRate
	}

	// Occurred at cannot be in the future
	if t.OccurredAt.After(time.Now()) {
		return ErrOccurredAtInFuture
	}

	return nil
}

// GetAmount returns the amount as *big.Int
func (t *ManualOutcomeTransaction) GetAmount() *big.Int {
	return t.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (t *ManualOutcomeTransaction) GetUSDRate() *big.Int {
	return t.USDRate.ToBigInt()
}

// HasManualPrice returns true if the transaction has a manually specified USD rate
func (t *ManualOutcomeTransaction) HasManualPrice() bool {
	return t.USDRate != nil && t.USDRate.Sign() > 0
}

// ParseOutcomeFromRawData parses a raw_data map into ManualOutcomeTransaction
func ParseOutcomeFromRawData(raw map[string]interface{}) (*ManualOutcomeTransaction, error) {
	tx := &ManualOutcomeTransaction{}

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

	// Parse price_asset_id
	if priceAssetID, ok := raw["price_asset_id"].(string); ok {
		tx.PriceAssetID = priceAssetID
	}

	// Parse amount
	if amountStr, ok := raw["amount"].(string); ok {
		amount, ok := money.NewBigIntFromString(amountStr)
		if !ok {
			return nil, ErrInvalidAmount
		}
		tx.Amount = amount
	}

	// Parse usd_rate (optional)
	if usdRateStr, ok := raw["usd_rate"].(string); ok && usdRateStr != "" {
		usdRate, ok := money.NewBigIntFromString(usdRateStr)
		if !ok {
			return nil, ErrInvalidUSDRate
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
