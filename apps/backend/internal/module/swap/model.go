package swap

import (
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// SwapTransaction represents a token swap (DEX) transaction
type SwapTransaction struct {
	WalletID     uuid.UUID       `json:"wallet_id"`
	TxHash       string          `json:"tx_hash"`
	ChainID      int64           `json:"chain_id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Protocol     string          `json:"protocol,omitempty"`
	TransfersIn  []SwapTransfer  `json:"transfers_in"`
	TransfersOut []SwapTransfer  `json:"transfers_out"`
	FeeAsset     string          `json:"fee_asset,omitempty"`
	FeeAmount    *money.BigInt   `json:"fee_amount,omitempty"`
	FeeDecimals  int             `json:"fee_decimals,omitempty"`
	FeeUSDPrice  *money.BigInt   `json:"fee_usd_price,omitempty"`
}

// SwapTransfer represents a single asset movement within a swap
type SwapTransfer struct {
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
	if t.ChainID <= 0 {
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
	if t.AssetSymbol == "" {
		return ErrInvalidAssetID
	}
	if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
