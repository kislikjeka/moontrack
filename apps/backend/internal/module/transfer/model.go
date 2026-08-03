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
	// AssetID is a registry UUID (#59): it becomes Entry.AssetID and the asset
	// segment of the wallet / income / expense codes this item produces.
	// AssetSymbol is the ticker, carried for display and nothing else.
	AssetID         uuid.UUID     `json:"asset_id"`
	AssetSymbol     string        `json:"asset_symbol,omitempty"`
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
	// uuid.Nil is rejected outright: an entry booked against it balances and is
	// silently against the wrong asset, which is what #59 removes.
	if ti.AssetID == uuid.Nil {
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

// GetUSDRate returns the USD rate as *big.Int, or nil when the price for this
// leg is not known.
//
// nil, not zero (#74). Sync omits usd_rate from raw_data precisely when it has
// no price and has enqueued a backfill job for it; substituting zero here made
// that indistinguishable from an asset genuinely worth nothing, and the zero
// travelled all the way into tax_lots.auto_cost_basis_per_unit with
// price_status='resolved' — a cost basis that was both wrong and marked final.
func (ti *TransferItem) GetUSDRate() *big.Int {
	return ti.USDRate.ToBigInt()
}

// TransferInTransaction represents an incoming blockchain transfer
type TransferInTransaction struct {
	WalletID        uuid.UUID     `json:"wallet_id"`
	AssetID         uuid.UUID     `json:"asset_id"`               // Legacy single-asset registry UUID (#59)
	AssetSymbol     string        `json:"asset_symbol,omitempty"` // Legacy single-asset ticker, display only
	Decimals        int           `json:"decimals"`               // Legacy single-asset decimals
	Amount          *money.BigInt `json:"amount"`                 // Legacy single-asset amount
	USDRate         *money.BigInt `json:"usd_rate"`               // Legacy single-asset USD rate
	ChainID         string        `json:"chain_id"`               // EVM chain ID
	TxHash          string        `json:"tx_hash"`                // Blockchain transaction hash
	BlockNumber     int64         `json:"block_number"`           // Block number
	FromAddress     string        `json:"from_address"`           // Legacy sender address
	ContractAddress string        `json:"contract_address"`       // Legacy contract address for ERC-20
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
		if t.AssetID == uuid.Nil {
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

// GetUSDRate returns the USD rate as *big.Int, or nil when unknown (#74).
func (t *TransferInTransaction) GetUSDRate() *big.Int {
	return t.USDRate.ToBigInt()
}

// TransferOutTransaction represents an outgoing blockchain transfer
type TransferOutTransaction struct {
	WalletID    uuid.UUID     `json:"wallet_id"`
	AssetID     uuid.UUID     `json:"asset_id"`               // Legacy single-asset registry UUID (#59)
	AssetSymbol string        `json:"asset_symbol,omitempty"` // Legacy single-asset ticker, display only
	Decimals    int           `json:"decimals"`               // Legacy single-asset decimals
	Amount      *money.BigInt `json:"amount"`                 // Legacy single-asset amount
	USDRate     *money.BigInt `json:"usd_rate"`               // Legacy single-asset USD rate
	GasAmount   *money.BigInt `json:"gas_amount"`             // Gas fee in native token base units
	GasUSDRate  *money.BigInt `json:"gas_usd_rate"`           // Native token USD rate scaled by 10^8
	// NativeAssetID is the registry UUID of the chain's gas token (#59). It was
	// a ticker with an "ETH" default, which meant a fee paid in MATIC was
	// charged to the ETH gas account on every chain that did not set it.
	// There is no default now: a gas fee with no native asset is an error.
	NativeAssetID     uuid.UUID `json:"native_asset_id"`
	NativeAssetSymbol string    `json:"native_asset_symbol,omitempty"` // display only
	ChainID           string    `json:"chain_id"`                      // EVM chain ID
	TxHash            string    `json:"tx_hash"`                       // Blockchain transaction hash
	BlockNumber       int64     `json:"block_number"`                  // Block number
	ToAddress         string    `json:"to_address"`                    // Legacy receiver address
	ContractAddress   string    `json:"contract_address"`              // Legacy contract address for ERC-20
	OccurredAt        time.Time `json:"occurred_at"`
	UniqueID          string    `json:"unique_id"` // Unique transfer ID from blockchain provider

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
		if t.AssetID == uuid.Nil {
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

// GetUSDRate returns the USD rate as *big.Int, or nil when unknown (#74).
func (t *TransferOutTransaction) GetUSDRate() *big.Int {
	return t.USDRate.ToBigInt()
}

// GetGasAmount returns the gas amount as *big.Int
func (t *TransferOutTransaction) GetGasAmount() *big.Int {
	if t.GasAmount == nil {
		return big.NewInt(0)
	}
	return t.GasAmount.ToBigInt()
}

// GetGasUSDRate returns the gas USD rate as *big.Int, or nil when unknown (#74).
func (t *TransferOutTransaction) GetGasUSDRate() *big.Int {
	return t.GasUSDRate.ToBigInt()
}

// InternalTransferTransaction represents a transfer between user's own wallets
type InternalTransferTransaction struct {
	SourceWalletID uuid.UUID     `json:"source_wallet_id"`
	DestWalletID   uuid.UUID     `json:"dest_wallet_id"`
	AssetID        uuid.UUID     `json:"asset_id"`               // Registry UUID of the moved asset (#59)
	AssetSymbol    string        `json:"asset_symbol,omitempty"` // display only
	Decimals       int           `json:"decimals"`               // Asset decimals
	Amount         *money.BigInt `json:"amount"`                 // Amount in base units
	USDRate        *money.BigInt `json:"usd_rate"`               // USD rate scaled by 10^8
	GasAmount      *money.BigInt `json:"gas_amount"`             // Gas fee in native token base units
	GasUSDRate     *money.BigInt `json:"gas_usd_rate"`           // Native token USD rate scaled by 10^8
	GasDecimals    int           `json:"gas_decimals"`           // Native token decimals
	// NativeAssetID is the registry UUID of the chain's gas token (#59); see
	// TransferOutTransaction for why the old "ETH" default is gone.
	NativeAssetID     uuid.UUID `json:"native_asset_id"`
	NativeAssetSymbol string    `json:"native_asset_symbol,omitempty"` // display only
	ChainID           string    `json:"chain_id"`                      // EVM chain ID
	TxHash            string    `json:"tx_hash"`                       // Blockchain transaction hash
	BlockNumber       int64     `json:"block_number"`                  // Block number
	ContractAddress   string    `json:"contract_address"`              // Contract address for ERC-20 (empty for native)
	OccurredAt        time.Time `json:"occurred_at"`
	UniqueID          string    `json:"unique_id"` // Unique transfer ID from blockchain provider

	// SourceChainID and DestChainID let one internal transfer span two chains
	// (a bridge of the user's own funds, see ADR-0002). Both are optional and
	// fall back to ChainID, which is the same-chain shape every raw written
	// before cross-chain support carries. Use SourceChain()/DestChain() rather
	// than reading these directly.
	SourceChainID string `json:"source_chain_id,omitempty"`
	DestChainID   string `json:"dest_chain_id,omitempty"`
}

// SourceChain returns the chain the asset leaves from, defaulting to ChainID.
func (t *InternalTransferTransaction) SourceChain() string {
	if t.SourceChainID != "" {
		return t.SourceChainID
	}
	return t.ChainID
}

// DestChain returns the chain the asset arrives on, defaulting to ChainID.
func (t *InternalTransferTransaction) DestChain() string {
	if t.DestChainID != "" {
		return t.DestChainID
	}
	return t.ChainID
}

// IsCrossChain reports whether this transfer crosses a chain boundary.
func (t *InternalTransferTransaction) IsCrossChain() bool {
	return t.SourceChain() != t.DestChain()
}

// Validate validates the internal transfer transaction
func (t *InternalTransferTransaction) Validate() error {
	if t.SourceWalletID == uuid.Nil {
		return ErrMissingSourceWallet
	}

	if t.DestWalletID == uuid.Nil {
		return ErrMissingDestWallet
	}

	// A transfer from a wallet to itself is a no-op that would fabricate a
	// disposal and a re-acquisition of the same lot — unless it crosses chains.
	// One wallet bridging its own funds (Base → Arbitrum) is the canonical
	// bridge case: same wallet, two genuinely different accounts.
	if t.SourceWalletID == t.DestWalletID && !t.IsCrossChain() {
		return ErrSameWalletTransfer
	}

	if t.AssetID == uuid.Nil {
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

// GetUSDRate returns the USD rate as *big.Int, or nil when unknown (#74).
func (t *InternalTransferTransaction) GetUSDRate() *big.Int {
	return t.USDRate.ToBigInt()
}

// GetGasAmount returns the gas amount as *big.Int
func (t *InternalTransferTransaction) GetGasAmount() *big.Int {
	if t.GasAmount == nil {
		return big.NewInt(0)
	}
	return t.GasAmount.ToBigInt()
}

// GetGasUSDRate returns the gas USD rate as *big.Int, or nil when unknown (#74).
func (t *InternalTransferTransaction) GetGasUSDRate() *big.Int {
	return t.GasUSDRate.ToBigInt()
}
