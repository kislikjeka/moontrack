package liquidity

import (
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// LPTransaction represents a liquidity pool transaction (deposit, withdraw, claim fees).
// Reuses the same transfer-based pattern as swap transactions.
type LPTransaction struct {
	WalletID    uuid.UUID    `json:"wallet_id"`
	TxHash      string       `json:"tx_hash"`
	ChainID     int64        `json:"chain_id"`
	OccurredAt  time.Time    `json:"occurred_at"`
	Protocol    string       `json:"protocol,omitempty"`
	NFTTokenID  string       `json:"nft_token_id,omitempty"`
	Transfers   []LPTransfer `json:"transfers"`
	FeeAsset    string       `json:"fee_asset,omitempty"`
	FeeAmount   *money.BigInt `json:"fee_amount,omitempty"`
	FeeDecimals int          `json:"fee_decimals,omitempty"`
	FeeUSDPrice *money.BigInt `json:"fee_usd_price,omitempty"`
}

// LPTransfer represents a single asset movement within an LP operation.
type LPTransfer struct {
	AssetSymbol     string        `json:"asset_symbol"`
	Amount          *money.BigInt `json:"amount"`
	Decimals        int           `json:"decimals"`
	USDPrice        *money.BigInt `json:"usd_price"`
	ContractAddress string        `json:"contract_address"`
	Direction       string        `json:"direction"` // "in" or "out"
}

// Validate validates the LP transaction data.
func (t *LPTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}
	if t.TxHash == "" {
		return ErrInvalidTxHash
	}
	if t.ChainID <= 0 {
		return ErrInvalidChainID
	}
	if len(t.Transfers) == 0 {
		return ErrNoTransfers
	}
	for _, tr := range t.Transfers {
		if err := tr.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateClaim validates an LP claim (requires at least one IN transfer).
func (t *LPTransaction) ValidateClaim() error {
	if err := t.Validate(); err != nil {
		return err
	}
	hasIn := false
	for _, tr := range t.Transfers {
		if tr.Direction == "in" {
			hasIn = true
			break
		}
	}
	if !hasIn {
		return ErrNoIncomingTransfers
	}
	return nil
}

// TransfersIn returns transfers with direction "in".
func (t *LPTransaction) TransfersIn() []LPTransfer {
	var result []LPTransfer
	for _, tr := range t.Transfers {
		if tr.Direction == "in" {
			result = append(result, tr)
		}
	}
	return result
}

// TransfersOut returns transfers with direction "out".
func (t *LPTransaction) TransfersOut() []LPTransfer {
	var result []LPTransfer
	for _, tr := range t.Transfers {
		if tr.Direction == "out" {
			result = append(result, tr)
		}
	}
	return result
}

// Validate validates a single LP transfer.
func (t *LPTransfer) Validate() error {
	if t.AssetSymbol == "" {
		return ErrInvalidAssetID
	}
	if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
