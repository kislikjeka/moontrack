package rawdata

import (
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// IncomeData represents parsed data from a manual income transaction's raw_data
type IncomeData struct {
	WalletID uuid.UUID
	// AssetID is the registry UUID (#59); AssetSymbol is the ticker beside it in
	// raw_data. This package feeds the transaction list and detail views only —
	// nothing parsed here reaches a ledger entry — so the symbol is what gets
	// rendered and the UUID is the stable key the API returns alongside it.
	AssetID      uuid.UUID
	AssetSymbol  string
	PriceAssetID string
	Decimals     int
	Amount       *money.BigInt
	USDRate      *money.BigInt
	OccurredAt   time.Time
	Notes        string
	PriceSource  string
}

// GetAmount returns the amount as *big.Int
func (d *IncomeData) GetAmount() *big.Int {
	return d.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (d *IncomeData) GetUSDRate() *big.Int {
	return d.USDRate.ToBigInt()
}

// ParseIncomeFromRawData parses a raw_data map into IncomeData
func ParseIncomeFromRawData(raw map[string]interface{}) (*IncomeData, error) {
	data := &IncomeData{}

	// Parse wallet_id
	if walletIDStr, ok := raw["wallet_id"].(string); ok {
		walletID, err := uuid.Parse(walletIDStr)
		if err != nil {
			return nil, ErrInvalidWalletID
		}
		data.WalletID = walletID
	}

	// Parse asset_id. A present-but-malformed id is an error: this is a read
	// path, but returning uuid.Nil would put a bogus id in the API response
	// where the caller cannot tell it apart from a real one (#59).
	if assetIDStr, ok := raw["asset_id"].(string); ok {
		assetID, err := uuid.Parse(assetIDStr)
		if err != nil {
			return nil, ErrInvalidAssetID
		}
		data.AssetID = assetID
	}

	// asset_symbol is display data — absent is fine, it renders blank.
	if symbol, ok := raw["asset_symbol"].(string); ok {
		data.AssetSymbol = symbol
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
