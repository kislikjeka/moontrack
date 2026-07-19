package sync

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/lendingposition"
	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
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

	// SetSyncPhase updates the wallet's sync phase
	SetSyncPhase(ctx context.Context, walletID uuid.UUID, phase string) error

	// SetCollectCursor updates the wallet's collect cursor timestamp
	SetCollectCursor(ctx context.Context, walletID uuid.UUID, cursor time.Time) error

	// WipeWalletLedger calls the wipe_wallet_ledger function to reset ledger data for replay
	WipeWalletLedger(ctx context.Context, walletID uuid.UUID) error

	// GetChainSyncRows returns the wallet's per-chain sync-state rows. These rows
	// ARE the wallet chain set: the collector and reconciler iterate exactly the
	// chains returned here (issue #27).
	GetChainSyncRows(ctx context.Context, walletID uuid.UUID) ([]wallet.WalletChainSync, error)

	// SetChainCollectCursor updates a single (wallet, chain) row's collect cursor.
	SetChainCollectCursor(ctx context.Context, walletID uuid.UUID, chain string, cursor time.Time) error

	// SetChainSyncError marks a single (wallet, chain) row as errored. It does NOT
	// touch that chain's collect cursor, so the failed chain resumes from where it
	// left off next cycle. Only this chain's row changes — sibling chains are
	// unaffected (issue #28 failure isolation).
	SetChainSyncError(ctx context.Context, walletID uuid.UUID, chain, errMsg string) error

	// SetChainSyncCompleted marks a single (wallet, chain) row as synced at syncAt,
	// clearing its error and returning its phase to idle. Per-chain analogue of
	// SetSyncCompletedAt (issue #28).
	SetChainSyncCompleted(ctx context.Context, walletID uuid.UUID, chain string, syncAt time.Time) error

	// RollupWalletSyncStatus derives the wallet-level sync_status from its per-chain
	// rows (wallet.RollupStatus fold: error if any chain errored, else syncing, else
	// synced, else pending) and writes it to the wallets row, along with the first
	// errored chain's message as sync_error (NULL if none). This keeps
	// wallets.sync_status a true rollup once chains advance independently (issue #28).
	RollupWalletSyncStatus(ctx context.Context, walletID uuid.UUID) error
}

// PositionDataProvider fetches on-chain positions (balances) from an external
// API. Chain-aware: the caller (Reconciler) owns the fan-out loop over a wallet's
// chain set and invokes this once per enabled chain (issue #27).
type PositionDataProvider interface {
	GetPositions(ctx context.Context, address, chain string) ([]OnChainPosition, error)
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
	ChainID       string
	OperationType OperationType
	Protocol      string // Protocol name (e.g. "Uniswap V3"), empty if unknown
	Transfers     []DecodedTransfer
	Fee           *DecodedFee // nil if fee info unavailable
	MinedAt       time.Time
	Status        string   // "confirmed", "pending", "failed"
	NFTTokenID    string   // Uniswap V3 NFT position ID, empty if not applicable
	Acts          []string // Action types from the provider acts array (e.g., ["claim", "execute"])

	// NeedsReview is set when the adapter could not convert a value exactly
	// (e.g. an amount carried more fractional digits than the token's decimals,
	// so base-unit conversion would truncate). The downstream processor routes
	// such transactions to manual review rather than silently flooring. Zero
	// (false) for every existing producer — additive, backward-compatible.
	NeedsReview bool
	// ReviewReason is a human-readable explanation of why NeedsReview is set;
	// empty when NeedsReview is false.
	ReviewReason string
}

// DecodedTransfer represents a single token movement within a decoded transaction
type DecodedTransfer struct {
	AssetSymbol     string
	AssetName       string // Human-readable name (e.g. "Ethereum"), empty if unknown
	ContractAddress string // Lowercase, empty for native tokens
	Decimals        int
	Amount          *big.Int          // Amount in base units (never nil)
	Direction       TransferDirection // "in" or "out"
	Sender          string            // Lowercase address
	Recipient       string            // Lowercase address
	USDPrice        *big.Int          // USD price scaled by 1e8, nil if unavailable
	IconURL         string            // Token icon URL, empty if unavailable
}

// DecodedFee represents the gas fee for a decoded transaction
type DecodedFee struct {
	AssetSymbol string
	AssetName   string   // Human-readable name, empty if unknown
	Amount      *big.Int // Amount in base units (never nil)
	Decimals    int
	USDPrice    *big.Int // USD price scaled by 1e8, nil if unavailable
	IconURL     string   // Token icon URL, empty if unavailable
}

// TransactionDataProvider fetches decoded transactions from an external API.
// GetTransactions is chain-aware: the caller (Collector) owns the fan-out loop
// over a wallet's chains and invokes the provider once per chain.
type TransactionDataProvider interface {
	GetTransactions(ctx context.Context, address, chain string, since time.Time) ([]DecodedTransaction, error)
}

// LPPositionService manages LP position lifecycle
type LPPositionService interface {
	FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, chainID, protocol, nftTokenID, contractAddress string, token0, token1 lpposition.TokenInfo, openedAt time.Time) (*lpposition.LPPosition, error)
	FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol, token0, token1 string) (*lpposition.LPPosition, error)
	RecordDeposit(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
	RecordWithdraw(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
	RecordClaimFees(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
}

// LendingPositionService manages lending position lifecycle
type LendingPositionService interface {
	FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, protocol, chainID string, openedAt time.Time) (*lendingposition.LendingPosition, error)
	RecordSupply(ctx context.Context, positionID uuid.UUID, asset string, decimals int, contract string, amount, usdValue *big.Int) error
	RecordWithdraw(ctx context.Context, positionID uuid.UUID, asset string, amount, usdValue *big.Int) error
	RecordBorrow(ctx context.Context, positionID uuid.UUID, asset string, decimals int, contract string, amount, usdValue *big.Int) error
	RecordRepay(ctx context.Context, positionID uuid.UUID, asset string, amount, usdValue *big.Int) error
	RecordClaim(ctx context.Context, positionID uuid.UUID, usdValue *big.Int) error
}

// AssetService defines asset operations for sync
type AssetService interface {
	// GetPriceBySymbol returns the current USD price for an asset by symbol (scaled by 10^8)
	// Returns nil if price unavailable (graceful degradation)
	GetPriceBySymbol(ctx context.Context, symbol string) (*big.Int, error)
}
