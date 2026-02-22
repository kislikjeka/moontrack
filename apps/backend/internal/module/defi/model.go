package defi

import (
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// DeFiTransaction represents a DeFi protocol interaction (deposit, withdraw, claim)
type DeFiTransaction struct {
	WalletID      uuid.UUID      `json:"wallet_id"`
	TxHash        string         `json:"tx_hash"`
	ChainID       string         `json:"chain_id"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Protocol      string         `json:"protocol,omitempty"`
	OperationType string         `json:"operation_type,omitempty"` // "deposit", "mint", "withdraw", "burn", "claim"
	Transfers     []DeFiTransfer `json:"transfers"`
	FeeAsset      string         `json:"fee_asset,omitempty"`
	FeeAmount     *money.BigInt  `json:"fee_amount,omitempty"`
	FeeDecimals   int            `json:"fee_decimals,omitempty"`
	FeeUSDPrice   *money.BigInt  `json:"fee_usd_price,omitempty"`
}

// DeFiTransfer represents a single asset movement within a DeFi transaction
type DeFiTransfer struct {
	AssetSymbol     string        `json:"asset_symbol"`
	Amount          *money.BigInt `json:"amount"`
	Decimals        int           `json:"decimals"`
	USDPrice        *money.BigInt `json:"usd_price"`
	Direction       string        `json:"direction"` // "in" or "out"
	ContractAddress string        `json:"contract_address"`
	Sender          string        `json:"sender"`
	Recipient       string        `json:"recipient"`
}

// Validate validates the DeFi transaction data (common validation for all DeFi types)
func (t *DeFiTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}
	if t.TxHash == "" {
		return ErrInvalidTxHash
	}
	if t.ChainID == "" {
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

// ValidateClaim validates that the transaction has at least one incoming transfer
func (t *DeFiTransaction) ValidateClaim() error {
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
		return ErrNoInTransfers
	}
	return nil
}

// TransfersIn returns only incoming transfers
func (t *DeFiTransaction) TransfersIn() []DeFiTransfer {
	result := make([]DeFiTransfer, 0)
	for _, tr := range t.Transfers {
		if tr.Direction == "in" {
			result = append(result, tr)
		}
	}
	return result
}

// TransfersOut returns only outgoing transfers
func (t *DeFiTransaction) TransfersOut() []DeFiTransfer {
	result := make([]DeFiTransfer, 0)
	for _, tr := range t.Transfers {
		if tr.Direction == "out" {
			result = append(result, tr)
		}
	}
	return result
}

// Validate validates a single DeFi transfer
func (t *DeFiTransfer) Validate() error {
	if t.AssetSymbol == "" {
		return ErrInvalidAssetID
	}
	if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	if t.Decimals < 0 {
		return ErrInvalidDecimals
	}
	return nil
}
