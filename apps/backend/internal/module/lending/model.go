package lending

import (
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// LendingTransaction represents an AAVE lending operation.
type LendingTransaction struct {
	WalletID        uuid.UUID     `json:"wallet_id"`
	TxHash          string        `json:"tx_hash"`
	ChainID         string        `json:"chain_id"`
	OccurredAt      time.Time     `json:"occurred_at"`
	Protocol        string        `json:"protocol,omitempty"`
	Asset           string        `json:"asset"`
	Amount          *money.BigInt `json:"amount"`
	Decimals        int           `json:"decimals"`
	USDPrice        *money.BigInt `json:"usd_price,omitempty"`
	ContractAddress string        `json:"contract_address,omitempty"`
	FeeAsset        string        `json:"fee_asset,omitempty"`
	FeeAmount       *money.BigInt `json:"fee_amount,omitempty"`
	FeeDecimals     int           `json:"fee_decimals,omitempty"`
	FeeUSDPrice     *money.BigInt `json:"fee_usd_price,omitempty"`
}

// Validate validates the lending transaction data.
func (t *LendingTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}
	if t.TxHash == "" {
		return ErrInvalidTxHash
	}
	if t.ChainID == "" {
		return ErrInvalidChainID
	}
	if t.Asset == "" {
		return ErrInvalidAsset
	}
	if t.Amount == nil || t.Amount.IsNil() || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	if t.Decimals <= 0 {
		return ErrInvalidDecimals
	}
	return nil
}
