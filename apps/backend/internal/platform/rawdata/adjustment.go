package rawdata

import (
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// AdjustmentData represents parsed data from an asset adjustment transaction's raw_data
type AdjustmentData struct {
	WalletID uuid.UUID
	// AssetID is the registry UUID (#59); AssetSymbol is the ticker beside it in
	// raw_data. This package feeds the transaction list and detail views only —
	// nothing parsed here reaches a ledger entry — so the symbol is what gets
	// rendered and the UUID is the stable key the API returns alongside it.
	AssetID     uuid.UUID
	AssetSymbol string
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
