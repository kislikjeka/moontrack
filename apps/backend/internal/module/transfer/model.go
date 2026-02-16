package transfer

import (
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// TransferInTransaction represents an incoming blockchain transfer
type TransferInTransaction struct {
	WalletID        uuid.UUID     `json:"wallet_id"`
	AssetID         string        `json:"asset_id"`         // Asset symbol (ETH, USDC, etc.)
	Decimals        int           `json:"decimals"`         // Asset decimals
	Amount          *money.BigInt `json:"amount"`           // Amount in base units
	USDRate         *money.BigInt `json:"usd_rate"`         // USD rate scaled by 10^8
	ChainID         int64         `json:"chain_id"`         // EVM chain ID
	TxHash          string        `json:"tx_hash"`          // Blockchain transaction hash
	BlockNumber     int64         `json:"block_number"`     // Block number
	FromAddress     string        `json:"from_address"`     // Sender address
	ContractAddress string        `json:"contract_address"` // Contract address for ERC-20 (empty for native)
	OccurredAt      time.Time     `json:"occurred_at"`
	UniqueID        string        `json:"unique_id"` // Unique transfer ID from blockchain provider
}

// Validate validates the transfer in transaction
func (t *TransferInTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
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

	if t.ChainID <= 0 {
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
	AssetID         string        `json:"asset_id"`         // Asset symbol (ETH, USDC, etc.)
	Decimals        int           `json:"decimals"`         // Asset decimals
	Amount          *money.BigInt `json:"amount"`           // Amount in base units
	USDRate         *money.BigInt `json:"usd_rate"`         // USD rate scaled by 10^8
	GasAmount       *money.BigInt `json:"gas_amount"`       // Gas fee in native token base units
	GasUSDRate      *money.BigInt `json:"gas_usd_rate"`     // Native token USD rate scaled by 10^8
	ChainID         int64         `json:"chain_id"`         // EVM chain ID
	TxHash          string        `json:"tx_hash"`          // Blockchain transaction hash
	BlockNumber     int64         `json:"block_number"`     // Block number
	ToAddress       string        `json:"to_address"`       // Receiver address
	ContractAddress string        `json:"contract_address"` // Contract address for ERC-20 (empty for native)
	OccurredAt      time.Time     `json:"occurred_at"`
	UniqueID        string        `json:"unique_id"` // Unique transfer ID from blockchain provider
}

// Validate validates the transfer out transaction
func (t *TransferOutTransaction) Validate() error {
	if t.WalletID == uuid.Nil {
		return ErrInvalidWalletID
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

	if t.ChainID <= 0 {
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
	ChainID         int64         `json:"chain_id"`        // EVM chain ID
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

	if t.ChainID <= 0 {
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
