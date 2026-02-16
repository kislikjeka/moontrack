package sync

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// TransferDirection indicates if a transfer is incoming or outgoing
type TransferDirection string

const (
	DirectionIn  TransferDirection = "in"
	DirectionOut TransferDirection = "out"
)

// LedgerService defines the interface for ledger operations needed by sync
type LedgerService interface {
	RecordTransaction(ctx context.Context, transactionType ledger.TransactionType, source string, externalID *string, occurredAt time.Time, rawData map[string]interface{}) (*ledger.Transaction, error)
}

// WalletRepository defines wallet data access for sync operations
type WalletRepository interface {
	// GetWalletsForSync retrieves wallets that need syncing
	GetWalletsForSync(ctx context.Context) ([]*wallet.Wallet, error)

	// GetWalletsByAddressAndUserID retrieves wallets with a given address for a specific user
	GetWalletsByAddressAndUserID(ctx context.Context, address string, userID uuid.UUID) ([]*wallet.Wallet, error)

	// ClaimWalletForSync atomically claims a wallet for syncing (returns false if already syncing)
	ClaimWalletForSync(ctx context.Context, walletID uuid.UUID) (bool, error)

	// SetSyncInProgress marks a wallet as syncing
	SetSyncInProgress(ctx context.Context, walletID uuid.UUID) error

	// SetSyncCompletedAt marks a wallet as synced at a given time
	SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error

	// SetSyncError marks a wallet sync as failed
	SetSyncError(ctx context.Context, walletID uuid.UUID, errMsg string) error
}

// isDuplicateError checks if the error is due to a unique constraint violation (PostgreSQL error code 23505)
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// OperationType represents the high-level category of a decoded transaction
type OperationType string

const (
	OpTrade    OperationType = "trade"
	OpDeposit  OperationType = "deposit"
	OpWithdraw OperationType = "withdraw"
	OpClaim    OperationType = "claim"
	OpReceive  OperationType = "receive"
	OpSend     OperationType = "send"
	OpExecute  OperationType = "execute"
	OpApprove  OperationType = "approve"
	OpMint     OperationType = "mint"
	OpBurn     OperationType = "burn"
)

// DecodedTransaction represents a fully decoded blockchain transaction
type DecodedTransaction struct {
	ID            string
	TxHash        string
	ChainID       int64
	OperationType OperationType
	Protocol      string // Protocol name (e.g. "Uniswap V3"), empty if unknown
	Transfers     []DecodedTransfer
	Fee           *DecodedFee // nil if fee info unavailable
	MinedAt       time.Time
	Status        string // "confirmed", "pending", "failed"
}

// DecodedTransfer represents a single token movement within a decoded transaction
type DecodedTransfer struct {
	AssetSymbol     string
	ContractAddress string // Lowercase, empty for native tokens
	Decimals        int
	Amount          *big.Int          // Amount in base units (never nil)
	Direction       TransferDirection // "in" or "out"
	Sender          string            // Lowercase address
	Recipient       string            // Lowercase address
	USDPrice        *big.Int          // USD price scaled by 1e8, nil if unavailable
}

// DecodedFee represents the gas fee for a decoded transaction
type DecodedFee struct {
	AssetSymbol string
	Amount      *big.Int // Amount in base units (never nil)
	Decimals    int
	USDPrice    *big.Int // USD price scaled by 1e8, nil if unavailable
}

// TransactionDataProvider fetches decoded transactions from an external API
type TransactionDataProvider interface {
	GetTransactions(ctx context.Context, address string, chainID int64, since time.Time) ([]DecodedTransaction, error)
}

// AssetService defines asset operations for sync
type AssetService interface {
	// GetPriceBySymbol returns the current USD price for an asset by symbol (scaled by 10^8)
	// Returns nil if price unavailable (graceful degradation)
	GetPriceBySymbol(ctx context.Context, symbol string, chainID int64) (*big.Int, error)
}
