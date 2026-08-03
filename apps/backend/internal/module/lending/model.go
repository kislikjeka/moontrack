package lending

import (
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// LendingTransferItem represents one asset movement within a lending tx.
// A single on-chain op can emit multiple transfers — a supply of two different
// assets in one call, for instance. Only PRINCIPAL movements arrive here: the
// receipt the protocol mints against them (aToken, debt token) is dropped at
// the provider boundary, so a supply that once produced a principal leg and an
// aToken leg now produces one leg (#57).
type LendingTransferItem struct {
	// AssetID is a registry UUID (#59). It becomes Entry.AssetID and the asset
	// segment of the collateral / liability / wallet codes, so the position a
	// lending op moves is now identified the same way everywhere. AssetSymbol
	// rides along for display and keys nothing.
	AssetID         uuid.UUID     `json:"asset_id"`
	AssetSymbol     string        `json:"asset_symbol,omitempty"`
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
	// Nil is rejected here so a lending pair can never book collateral against
	// an asset that does not exist (#59).
	if t.AssetID == uuid.Nil {
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

// GetUSDRate returns the USD rate as *big.Int, or nil when unknown (#74).
func (t *LendingTransferItem) GetUSDRate() *big.Int {
	return t.USDRate.ToBigInt()
}

// LendingTransaction represents an AAVE lending operation.
type LendingTransaction struct {
	WalletID   uuid.UUID `json:"wallet_id"`
	TxHash     string    `json:"tx_hash"`
	ChainID    string    `json:"chain_id"`
	OccurredAt time.Time `json:"occurred_at"`
	Protocol   string    `json:"protocol,omitempty"`
	// FeeAsset is the registry UUID of the gas token (#59).
	FeeAsset       uuid.UUID     `json:"fee_asset,omitempty"`
	FeeAssetSymbol string        `json:"fee_asset_symbol,omitempty"`
	FeeAmount      *money.BigInt `json:"fee_amount,omitempty"`
	FeeDecimals    int           `json:"fee_decimals,omitempty"`
	FeeUSDPrice    *money.BigInt `json:"fee_usd_price,omitempty"`

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
