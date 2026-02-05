package sync

import (
	"context"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// TransferDirection indicates if a transfer is incoming or outgoing
type TransferDirection string

const (
	DirectionIn  TransferDirection = "in"
	DirectionOut TransferDirection = "out"
)

// TransferType indicates the blockchain transfer category
type TransferType string

const (
	TransferTypeExternal TransferType = "external" // Native token transfer
	TransferTypeInternal TransferType = "internal" // Internal contract call
	TransferTypeERC20    TransferType = "erc20"    // ERC-20 token transfer
)

// Transfer represents a blockchain transfer from the blockchain client
type Transfer struct {
	TxHash          string            // Transaction hash
	BlockNumber     int64             // Block number
	Timestamp       time.Time         // Block timestamp
	From            string            // Sender address (lowercase)
	To              string            // Receiver address (lowercase)
	Amount          *big.Int          // Amount in base units
	AssetSymbol     string            // Asset symbol (ETH, USDC, etc.)
	ContractAddress string            // Contract address for ERC-20 (empty for native)
	Decimals        int               // Token decimals
	ChainID         int64             // Chain ID
	Direction       TransferDirection // In or Out
	TransferType    TransferType      // External, Internal, ERC20
	UniqueID        string            // Unique transfer identifier from Alchemy
}

// BlockchainClient defines the interface for blockchain data providers
type BlockchainClient interface {
	// GetCurrentBlock returns the current block number for a chain
	GetCurrentBlock(ctx context.Context, chainID int64) (int64, error)

	// GetTransfers retrieves all transfers for an address within a block range
	GetTransfers(ctx context.Context, chainID int64, address string, fromBlock, toBlock int64) ([]Transfer, error)

	// GetNativeAssetInfo returns native asset symbol and decimals for a chain
	GetNativeAssetInfo(chainID int64) (symbol string, decimals int, err error)

	// IsSupported checks if a chain is supported
	IsSupported(chainID int64) bool
}

// WalletRepository defines wallet data access for sync operations
type WalletRepository interface {
	// GetWalletsForSync retrieves wallets that need syncing
	GetWalletsForSync(ctx context.Context) ([]*wallet.Wallet, error)

	// GetWalletsByAddress retrieves all wallets with a given address (for internal transfer detection)
	GetWalletsByAddress(ctx context.Context, address string) ([]*wallet.Wallet, error)

	// SetSyncInProgress marks a wallet as syncing
	SetSyncInProgress(ctx context.Context, walletID uuid.UUID) error

	// SetSyncCompleted marks a wallet as successfully synced
	SetSyncCompleted(ctx context.Context, walletID uuid.UUID, lastBlock int64, syncAt time.Time) error

	// SetSyncError marks a wallet sync as failed
	SetSyncError(ctx context.Context, walletID uuid.UUID, errMsg string) error
}

// AssetService defines asset operations for sync
type AssetService interface {
	// GetPriceBySymbol returns the current USD price for an asset by symbol (scaled by 10^8)
	// Returns nil if price unavailable (graceful degradation)
	GetPriceBySymbol(ctx context.Context, symbol string, chainID int64) (*big.Int, error)
}
