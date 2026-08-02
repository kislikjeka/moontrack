package lending

import (
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// AssetRole classifies an on-chain asset within a lending operation.
//
// Deprecated: unused. The entry builders no longer route by asset role — every
// leg is booked as the principal (#44). This symbol-prefix matcher is one of
// the four scheduled for removal in #57, which replaces it with the per-leg
// action the provider already sends; it is kept only so that removal lands as
// one change with its three siblings.
type AssetRole int

const (
	// RoleLiquid is a plain ERC-20 or native asset (USDC, ETH, DAI, ...)
	// that moves in/out of the user's wallet account.
	RoleLiquid AssetRole = iota
	// RoleCollateralReceipt is a supply-side receipt token (Aave aToken:
	// aEthWETH, aBasUSDC, ...) that tracks the user's supplied-asset claim.
	RoleCollateralReceipt
	// RoleLiabilityReceipt is a borrow-side debt token (variableDebt*,
	// stableDebt*) that tracks the user's outstanding debt balance.
	RoleLiabilityReceipt
)

// ClassifyLendingAsset decides whether a lending-operation asset is a liquid
// movable token or a receipt / debt token. Simple prefix match — reliable for
// Aave's well-known symbol conventions; future protocols may need extension.
//
// Deprecated: unused, see [AssetRole].
func ClassifyLendingAsset(symbol, _protocol string) AssetRole {
	switch {
	case strings.HasPrefix(symbol, "variableDebt"), strings.HasPrefix(symbol, "stableDebt"):
		return RoleLiabilityReceipt
	case len(symbol) > 2 && symbol[0] == 'a' && symbol[1] >= 'A' && symbol[1] <= 'Z':
		// aToken pattern: lowercase 'a' + uppercase letter (aEthWETH, aBasUSDC).
		return RoleCollateralReceipt
	default:
		return RoleLiquid
	}
}

// LendingTransferItem represents one asset movement within a lending tx.
// A single on-chain op can emit multiple transfers (debt receipt + real
// asset on borrow, aToken + principal on supply, etc).
type LendingTransferItem struct {
	AssetID         string        `json:"asset_id"`
	Decimals        int           `json:"decimals"`
	Amount          *money.BigInt `json:"amount"`
	USDRate         *money.BigInt `json:"usd_rate,omitempty"`
	ContractAddress string        `json:"contract_address,omitempty"`
	FromAddress     string        `json:"from_address,omitempty"`
	ToAddress       string        `json:"to_address,omitempty"`
	Direction       string        `json:"direction,omitempty"`
}

// Validate validates a single lending transfer item.
func (t *LendingTransferItem) Validate() error {
	if t.AssetID == "" {
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

// GetAmount returns the amount as *big.Int (never nil).
func (t *LendingTransferItem) GetAmount() *big.Int {
	if t.Amount == nil {
		return big.NewInt(0)
	}
	return t.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int (zero when absent).
func (t *LendingTransferItem) GetUSDRate() *big.Int {
	if t.USDRate == nil {
		return big.NewInt(0)
	}
	return t.USDRate.ToBigInt()
}

// LendingTransaction represents an AAVE lending operation.
type LendingTransaction struct {
	WalletID    uuid.UUID     `json:"wallet_id"`
	TxHash      string        `json:"tx_hash"`
	ChainID     string        `json:"chain_id"`
	OccurredAt  time.Time     `json:"occurred_at"`
	Protocol    string        `json:"protocol,omitempty"`
	FeeAsset    string        `json:"fee_asset,omitempty"`
	FeeAmount   *money.BigInt `json:"fee_amount,omitempty"`
	FeeDecimals int           `json:"fee_decimals,omitempty"`
	FeeUSDPrice *money.BigInt `json:"fee_usd_price,omitempty"`

	// Transfers holds all asset movements emitted by the on-chain op.
	// The handler books one balanced pair per item; a lending op always
	// carries at least one, since the classifier drops zero-transfer
	// transactions before they reach a handler.
	Transfers []LendingTransferItem `json:"transfers"`
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
	if len(t.Transfers) == 0 {
		return ErrNoTransfers
	}
	for i := range t.Transfers {
		if err := t.Transfers[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}
