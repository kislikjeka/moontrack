package transfer

import (
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// TransferItem is one asset movement within a blockchain transaction. A single
// on-chain tx can carry multiple asset movements (e.g. multi-asset send, or a
// DeFi call that emits several token transfers). The sync layer collects them
// into `Transfers` on TransferIn/OutTransaction so each movement becomes its
// own balanced ledger entry pair instead of being silently dropped.
type TransferItem struct {
	AssetID         string        `json:"asset_id"`
	Decimals        int           `json:"decimals"`
	Amount          *money.BigInt `json:"amount"`
	USDRate         *money.BigInt `json:"usd_rate,omitempty"`
	ContractAddress string        `json:"contract_address,omitempty"`
	FromAddress     string        `json:"from_address,omitempty"`
	ToAddress       string        `json:"to_address,omitempty"`
	Direction       string        `json:"direction,omitempty"` // "in" or "out"
}

// Validate validates a single transfer item.
func (ti *TransferItem) Validate() error {
	if ti.AssetID == "" {
		return ErrInvalidAssetID
	}
	if ti.Amount == nil || ti.Amount.IsNil() || ti.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	if ti.USDRate != nil && !ti.USDRate.IsNil() && ti.USDRate.Sign() < 0 {
		return ErrInvalidUSDRate
	}
	return nil
}

// GetAmount returns the amount as *big.Int (never nil).
func (ti *TransferItem) GetAmount() *big.Int {
	if ti.Amount == nil {
		return big.NewInt(0)
	}
	return ti.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int (zero when absent).
func (ti *TransferItem) GetUSDRate() *big.Int {
	if ti.USDRate == nil {
		return big.NewInt(0)
	}
	return ti.USDRate.ToBigInt()
}

// TransferInTransaction represents an incoming blockchain transfer
type TransferInTransaction struct {
	WalletID        uuid.UUID     `json:"wallet_id"`
	AssetID         string        `json:"asset_id"`         // Legacy single-asset symbol
	Decimals        int           `json:"decimals"`         // Legacy single-asset decimals
	Amount          *money.BigInt `json:"amount"`           // Legacy single-asset amount
	USDRate         *money.BigInt `json:"usd_rate"`         // Legacy single-asset USD rate
	ChainID         string        `json:"chain_id"`         // EVM chain ID
	TxHash          string        `json:"tx_hash"`          // Blockchain transaction hash
	BlockNumber     int64         `json:"block_number"`     // Block number
	FromAddress     string        `json:"from_address"`     // Legacy sender address
	ContractAddress string        `json:"contract_address"` // Legacy contract address for ERC-20
	OccurredAt      time.Time     `json:"occurred_at"`
	UniqueID        string        `json:"unique_id"` // Unique transfer ID from blockchain provider

	// Transfers holds one item per asset movement. When non-empty the handler
	// iterates this slice and ignores the legacy flat AssetID/Amount/etc.
	// Old raw_transactions rows written before multi-asset support land here
	// empty and fall back to the legacy path.
	Transfers []TransferItem `json:"transfers,omitempty"`
}

// Validate validates the transfer in transaction. When Transfers is populated,
// each item is validated and the flat AssetID/Amount fields are ignored.
// Otherwise the legacy flat fields must be valid (single-asset shape).
func (t *TransferInTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}

	if len(t.Transfers) > 0 {
		for i := range t.Transfers {
			if err := t.Transfers[i].Validate(); err != nil {
				return err
			}
		}
	} else {
		if t.AssetID == "" {
			return ErrInvalidAssetID
		}

		if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
			return ErrInvalidAmount
		}

		if !t.USDRate.IsNil() && t.USDRate.Sign() < 0 {
			return ErrInvalidUSDRate
		}
	}

	if t.OccurredAt.After(time.Now()) {
		return ErrOccurredAtInFuture
	}

	if t.TxHash == "" {
		return ErrInvalidTxHash
	}

	if t.BlockNumber < 0 {
		return ErrInvalidBlockNumber
	}

	if t.ChainID == "" {
		return ErrInvalidChainID
	}

	return nil
}

// GetAmount returns the amount as *big.Int
func (t *TransferInTransaction) GetAmount() *big.Int {
	return t.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (t *TransferInTransaction) GetUSDRate() *big.Int {
	if t.USDRate == nil {
		return big.NewInt(0)
	}
	return t.USDRate.ToBigInt()
}

// TransferOutTransaction represents an outgoing blockchain transfer
type TransferOutTransaction struct {
	WalletID        uuid.UUID     `json:"wallet_id"`
	AssetID         string        `json:"asset_id"`         // Legacy single-asset symbol
	Decimals        int           `json:"decimals"`         // Legacy single-asset decimals
	Amount          *money.BigInt `json:"amount"`           // Legacy single-asset amount
	USDRate         *money.BigInt `json:"usd_rate"`         // Legacy single-asset USD rate
	GasAmount       *money.BigInt `json:"gas_amount"`       // Gas fee in native token base units
	GasUSDRate      *money.BigInt `json:"gas_usd_rate"`     // Native token USD rate scaled by 10^8
	ChainID         string        `json:"chain_id"`         // EVM chain ID
	TxHash          string        `json:"tx_hash"`          // Blockchain transaction hash
	BlockNumber     int64         `json:"block_number"`     // Block number
	ToAddress       string        `json:"to_address"`       // Legacy receiver address
	ContractAddress string        `json:"contract_address"` // Legacy contract address for ERC-20
	OccurredAt      time.Time     `json:"occurred_at"`
	UniqueID        string        `json:"unique_id"` // Unique transfer ID from blockchain provider

	// Transfers holds one item per asset movement (multi-asset sends).
	// When non-empty the handler iterates this slice and ignores the legacy
	// flat AssetID/Amount/etc. fields.
	Transfers []TransferItem `json:"transfers,omitempty"`
}

// Validate validates the transfer out transaction. When Transfers is populated,
// each item is validated and the flat AssetID/Amount fields are ignored.
func (t *TransferOutTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
	}

	if len(t.Transfers) > 0 {
		for i := range t.Transfers {
			if err := t.Transfers[i].Validate(); err != nil {
				return err
			}
		}
	} else {
		if t.AssetID == "" {
			return ErrInvalidAssetID
		}

		if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
			return ErrInvalidAmount
		}

		if !t.USDRate.IsNil() && t.USDRate.Sign() < 0 {
			return ErrInvalidUSDRate
		}
	}

	if t.OccurredAt.After(time.Now()) {
		return ErrOccurredAtInFuture
	}

	if t.TxHash == "" {
		return ErrInvalidTxHash
	}

	if t.BlockNumber < 0 {
		return ErrInvalidBlockNumber
	}

	if t.ChainID == "" {
		return ErrInvalidChainID
	}

	return nil
}

// GetAmount returns the amount as *big.Int
func (t *TransferOutTransaction) GetAmount() *big.Int {
	return t.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (t *TransferOutTransaction) GetUSDRate() *big.Int {
	if t.USDRate == nil {
		return big.NewInt(0)
	}
	return t.USDRate.ToBigInt()
}

// GetGasAmount returns the gas amount as *big.Int
func (t *TransferOutTransaction) GetGasAmount() *big.Int {
	if t.GasAmount == nil {
		return big.NewInt(0)
	}
	return t.GasAmount.ToBigInt()
}

// GetGasUSDRate returns the gas USD rate as *big.Int
func (t *TransferOutTransaction) GetGasUSDRate() *big.Int {
	if t.GasUSDRate == nil {
		return big.NewInt(0)
	}
	return t.GasUSDRate.ToBigInt()
}

// InternalTransferTransaction represents a transfer between user's own wallets
type InternalTransferTransaction struct {
	SourceWalletID  uuid.UUID     `json:"source_wallet_id"`
	DestWalletID    uuid.UUID     `json:"dest_wallet_id"`
	AssetID         string        `json:"asset_id"`     // Asset symbol (ETH, USDC, etc.)
	Decimals        int           `json:"decimals"`     // Asset decimals
	Amount          *money.BigInt `json:"amount"`       // Amount in base units
	USDRate         *money.BigInt `json:"usd_rate"`     // USD rate scaled by 10^8
	GasAmount       *money.BigInt `json:"gas_amount"`   // Gas fee in native token base units
	GasUSDRate      *money.BigInt `json:"gas_usd_rate"` // Native token USD rate scaled by 10^8
	GasDecimals     int           `json:"gas_decimals"` // Native token decimals
	NativeAssetID   string        `json:"native_asset_id"` // Native asset symbol (ETH, MATIC, etc.)
	ChainID         string        `json:"chain_id"`        // EVM chain ID
	TxHash          string        `json:"tx_hash"`         // Blockchain transaction hash
	BlockNumber     int64         `json:"block_number"`    // Block number
	ContractAddress string        `json:"contract_address"` // Contract address for ERC-20 (empty for native)
	OccurredAt      time.Time     `json:"occurred_at"`
	UniqueID        string        `json:"unique_id"` // Unique transfer ID from blockchain provider
}

// Validate validates the internal transfer transaction
func (t *InternalTransferTransaction) Validate() error {
	if t.SourceWalletID == uuid.Nil {
		return ErrMissingSourceWallet
	}

	if t.DestWalletID == uuid.Nil {
		return ErrMissingDestWallet
	}

	if t.SourceWalletID == t.DestWalletID {
		return ErrSameWalletTransfer
	}

	if t.AssetID == "" {
		return ErrInvalidAssetID
	}

	if t.Amount.IsNil() || t.Amount.Sign() <= 0 {
		return ErrInvalidAmount
	}

	if !t.USDRate.IsNil() && t.USDRate.Sign() < 0 {
		return ErrInvalidUSDRate
	}

	if t.OccurredAt.After(time.Now()) {
		return ErrOccurredAtInFuture
	}

	if t.TxHash == "" {
		return ErrInvalidTxHash
	}

	if t.BlockNumber < 0 {
		return ErrInvalidBlockNumber
	}

	if t.ChainID == "" {
		return ErrInvalidChainID
	}

	return nil
}

// GetAmount returns the amount as *big.Int
func (t *InternalTransferTransaction) GetAmount() *big.Int {
	return t.Amount.ToBigInt()
}

// GetUSDRate returns the USD rate as *big.Int
func (t *InternalTransferTransaction) GetUSDRate() *big.Int {
	if t.USDRate == nil {
		return big.NewInt(0)
	}
	return t.USDRate.ToBigInt()
}

// GetGasAmount returns the gas amount as *big.Int
func (t *InternalTransferTransaction) GetGasAmount() *big.Int {
	if t.GasAmount == nil {
		return big.NewInt(0)
	}
	return t.GasAmount.ToBigInt()
}

// GetGasUSDRate returns the gas USD rate as *big.Int
func (t *InternalTransferTransaction) GetGasUSDRate() *big.Int {
	if t.GasUSDRate == nil {
		return big.NewInt(0)
	}
	return t.GasUSDRate.ToBigInt()
}
