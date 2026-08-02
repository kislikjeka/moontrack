package swap

import (
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// SwapTransaction represents a token swap (DEX) transaction
type SwapTransaction struct {
	WalletID     uuid.UUID      `json:"wallet_id"`
	TxHash       string         `json:"tx_hash"`
	ChainID      string         `json:"chain_id"`
	OccurredAt   time.Time      `json:"occurred_at"`
	Protocol     string         `json:"protocol,omitempty"`
	TransfersIn  []SwapTransfer `json:"transfers_in"`
	TransfersOut []SwapTransfer `json:"transfers_out"`
	// FeeAsset is the registry UUID of the token the gas was paid in (#59).
	// It keys the gas account code; the ticker beside it is display only. A
	// fee paid in MATIC previously landed in `gas.polygon.ETH` because the
	// ticker was the identity.
	FeeAsset       uuid.UUID     `json:"fee_asset,omitempty"`
	FeeAssetSymbol string        `json:"fee_asset_symbol,omitempty"`
	FeeAmount      *money.BigInt `json:"fee_amount,omitempty"`
	FeeDecimals    int           `json:"fee_decimals,omitempty"`
	FeeUSDPrice    *money.BigInt `json:"fee_usd_price,omitempty"`
}

// SwapTransfer represents a single asset movement within a swap.
//
// AssetID is identity — it becomes Entry.AssetID and the asset segment of the
// wallet and clearing account codes. AssetSymbol is the ticker, kept only so
// transaction views have something a human recognises (#59).
type SwapTransfer struct {
	AssetID         uuid.UUID     `json:"asset_id"`
	AssetSymbol     string        `json:"asset_symbol"`
	Amount          *money.BigInt `json:"amount"`
	Decimals        int           `json:"decimals"`
	USDPrice        *money.BigInt `json:"usd_price"`
	ContractAddress string        `json:"contract_address"`
	Sender          string        `json:"sender"`
	Recipient       string        `json:"recipient"`
}

// Validate validates the swap transaction data
func (t *SwapTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}
	if t.TxHash == "" {
		return ErrInvalidTxHash
	}
	if t.ChainID == "" {
		return ErrInvalidChainID
	}
	if len(t.TransfersIn) == 0 || len(t.TransfersOut) == 0 {
		return ErrNoTransfers
	}
	for _, tr := range t.TransfersIn {
		if err := tr.Validate(); err != nil {
			return err
		}
	}
	for _, tr := range t.TransfersOut {
		if err := tr.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate validates a single swap transfer
func (t *SwapTransfer) Validate() error {
	// Nil is rejected here rather than defaulted anywhere downstream: an entry
	// carrying uuid.Nil would be a balanced pair against an asset that does not
	// exist, which reconciles cleanly and is silently wrong (#59).
	if t.AssetID == uuid.Nil {
		return ErrInvalidAssetID
	}
	if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
