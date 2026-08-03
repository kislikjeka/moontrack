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

	// FindBySourceExternalID resolves the transaction already recorded under a
	// (source, external_id) pair, or (nil, nil) when none exists.
	//
	// Sync needs this because idempotency is global per on-chain event while
	// raw transactions are wallet-scoped: when one of the user's wallets does
	// not own the ledger transaction for an event it nonetheless observed —
	// a duplicate-skip, or the incoming side of an internal transfer — it must
	// still learn which transaction that event became, so its raw can reference
	// it (issue #31).
	FindBySourceExternalID(ctx context.Context, source, externalID string) (*ledger.Transaction, error)
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

// ErrSharedTxPending reports that a raw transaction describes an on-chain event
// owned by another of the user's wallets which has not recorded it yet — most
// often the incoming side of an internal transfer whose source wallet has not
// synced. It is a deferral, not a failure: the raw stays pending so a later
// cycle resolves it once the counterpart exists (issue #31).
var ErrSharedTxPending = errors.New("shared transaction not recorded yet")

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

	// LegActions holds the per-leg action of EVERY leg the provider sent,
	// including the receipt legs the adapter then dropped and the NFT-only legs
	// that never become transfers. It is what identifies the SHAPE of a
	// protocol interaction — a `collateralSharesMinted` anywhere in the
	// transaction means lending market, a `liquidityAdded` means liquidity pool
	// — and it survives the leg drop precisely so the evidence for the drop
	// outlives it.
	//
	// It replaces the protocol-name matching this classification used to rely
	// on. `protocol.name` is null on most real data, so the old path guessed
	// from party and NFT names against two hardcoded markers ("Uniswap V3",
	// "Aave"), which recognized exactly two protocols and silently degraded
	// every other one. The actions are the provider's own vocabulary and carry
	// no protocol names at all.
	//
	// Distinct from Acts, which mixes the transaction type and a synthetic
	// "claim" marker into the same flat list; LegActions is legs only.
	LegActions []string

	// NeedsReview is set when the adapter could not convert a value exactly
	// (e.g. an amount carried more fractional digits than the token's decimals,
	// so base-unit conversion would truncate). The downstream processor routes
	// such transactions to manual review rather than silently flooring. Zero
	// (false) for every existing producer — additive, backward-compatible.
	NeedsReview bool
	// ReviewReason is a human-readable explanation of why NeedsReview is set;
	// empty when NeedsReview is false.
	ReviewReason string

	// Unclassified reports that the sync provider could not identify what this
	// transaction did. Such a transaction still carries transfers, so it is not
	// dropped — it routes through the execute / in-out fallback. But the flag
	// must survive the port: OperationType collapses several distinct provider
	// types onto OpExecute, so downstream cannot otherwise tell an unknown
	// shape from a known one, and the both-direction case is exactly where that
	// distinction decides whether PnL is real or phantom. Zero (false) for
	// every classified transaction.
	Unclassified bool
	// ProviderType is the provider's own raw classification string, carried
	// verbatim for the audit trail. It separates the two populations inside
	// Unclassified that need opposite responses: the provider admitting it does
	// not know ("unclassified") means build handling for that shape, while a
	// type the adapter has no mapping for means the adapter is out of date.
	// Empty when the provider supplies no type.
	ProviderType string

	// DestChainID names the chain the asset ARRIVES on, when that differs from
	// ChainID (the chain this transaction was observed on). It is set only for a
	// bridge of the user's own funds stitched into a single cross-chain
	// internal_transfer (ADR-0002), where one transaction legitimately spans two
	// chains: the source-chain outflow and the destination-chain inflow.
	//
	// Empty for every ordinary transaction — including a same-chain internal
	// transfer — in which case the destination is ChainID. No sync provider
	// sets this; the provider decodes each bridge leg as an independent
	// single-chain transaction and links them in neither direction. It is
	// populated by MoonTrack's own bridge stitching (issue #33), which is the
	// only thing that can know both chains.
	DestChainID string

	// RejectedLegs records every leg this transaction carried that was
	// deliberately kept OUT of the ledger, with the identity of the asset and the
	// rule that rejected it (issue #60).
	//
	// It exists because rejection happens PER LEG while the raw's
	// processing_status describes the whole transaction: one transaction routinely
	// carries a principal leg that is booked and a receipt or spam leg that is
	// not, and `processed` cannot say that. Without this field the fact is
	// unrecoverable — the receipt rule (#57) drops its leg inside the provider
	// adapter, before the raw is ever written, so nothing downstream can
	// re-derive what was dropped. LegActions preserves THAT a receipt existed but
	// not WHICH asset it was, and the asset is precisely what reconciliation needs
	// in order to match the rejection against an on-chain position.
	//
	// It is a SEPARATE slice rather than a flag on Transfers on purpose. Transfers
	// means "principal legs eligible for the ledger" and is read by the
	// classifier, the bridge stitcher and every builder; re-admitting rejected
	// legs to it under a flag would put the burden of remembering to skip them on
	// forty-odd call sites, and one forgotten check books spam into the ledger.
	// Kept apart, the default stays correct everywhere and only the two readers
	// that WANT rejections look.
	//
	// Empty for an ordinary transaction, so it costs nothing on the common path.
	RejectedLegs []RejectedLeg
}

// RejectionReason names the rule that kept a leg out of the ledger.
//
// It is carried rather than collapsed to a boolean because the reconciliation
// report (#41, #61) must ATTRIBUTE an absence, not merely observe it: a position
// absent from the ledger because it is a protocol receipt is correct
// double-entry, a position absent because its asset is unknown is a filter
// decision to be listed by name, and a position absent for NEITHER reason is the
// only red one. One shared vocabulary is what lets the report and the per-chain
// flag explain the same fact the same way.
type RejectionReason string

const (
	// RejectionReceipt — the receipt rule (#57): a token the protocol minted to
	// record a position it is already holding for the user. Booking it beside the
	// principal it was minted against counts one supply twice.
	RejectionReceipt RejectionReason = "receipt"

	// RejectionUnknownAsset — the known-asset filter (#58): the asset has no
	// verdict yet, or a verdict of unknown, so it may not enter the ledger.
	RejectionUnknownAsset RejectionReason = "unknown_asset"
)

// RejectedLeg is one leg excluded from the ledger, carried far enough to explain
// the exclusion afterwards.
//
// The identity fields are the whole point. Reconciliation compares a rejection
// against an on-chain POSITION, and a position is identified by (chain,
// contract); a rejection recorded without its contract explains nothing.
type RejectedLeg struct {
	// ChainID and ContractAddress form the AssetKey the rejection applies to.
	// ChainID is carried explicitly rather than inherited from the transaction
	// because a stitched bridge's inbound leg belongs to the DESTINATION chain,
	// which is the same attribution the identity resolve and the known-asset
	// filter already use.
	ChainID         string
	ContractAddress string

	// AssetSymbol and Decimals are display metadata, never identifiers — the
	// same rule the ledger has followed since #59. Decimals travels with the
	// amount because a base-unit magnitude cannot be rendered without it.
	AssetSymbol string
	Decimals    int

	// Amount is the leg's quantity in base units and Direction is which way it
	// moved, kept so the report can show the SIZE of what was dropped rather than
	// only its name. Amount is nil for a leg the provider sent with no
	// convertible amount, such as an NFT-only receipt.
	Amount    *big.Int
	Direction TransferDirection

	// Reason names the rule that rejected the leg.
	Reason RejectionReason

	// Action is the provider's own name for what the leg did. It is kept for a
	// receipt so the EVIDENCE for the drop outlives the leg itself; empty for a
	// rejection that did not turn on the action.
	Action string
}

// Key returns the asset identity this rejection applies to.
func (r RejectedLeg) Key() AssetKey {
	return NewAssetKey(r.ChainID, r.ContractAddress)
}

// DecodedTransfer represents a single token movement within a decoded transaction
type DecodedTransfer struct {
	AssetSymbol string
	AssetName   string // Human-readable name (e.g. "Ethereum"), empty if unknown
	// ContractAddress is the lowercased contract, or NativeContract ("native")
	// for a chain's native coin. Adapters normalize into this contract, so a
	// leg arriving with an empty string means the provider supplied no token at
	// all — not that the leg is native. Together with ChainID it forms the
	// AssetKey that resolves to the asset's registry UUID.
	ContractAddress string
	Decimals        int
	Amount          *big.Int          // Amount in base units (never nil)
	Direction       TransferDirection // "in" or "out"
	Sender          string            // Lowercase address
	Recipient       string            // Lowercase address
	USDPrice        *big.Int          // USD price scaled by 1e8, nil if unavailable
	IconURL         string            // Token icon URL, empty if unavailable

	// Action is the provider's own name for what this specific leg DID —
	// `deposited`, `collateralSharesMinted`, `liquidityAdded`, `lpTokenBurned`,
	// `rewardsReceived`, `received`, … Empty when the provider stamps none.
	//
	// It is the one signal that separates a protocol RECEIPT (an aToken, a debt
	// token, an LP token: a claim the protocol mints against what it is already
	// holding for you) from the PRINCIPAL it was minted against. That question
	// is per-leg and cannot be answered anywhere else: it is not a property of
	// the token — the receipt token is real and quoted, and the known-asset
	// criterion says so correctly — but of the role the leg plays here.
	//
	// The transaction-level Acts slice cannot serve. It is built by flattening
	// every leg's action into one distinct list, which discards precisely the
	// leg attribution the rule needs (issue #37 read that flattened list and
	// concluded, correctly for it, that no per-leg signal existed). The signal
	// was never in Acts; it was in the raw per-leg field all along, and this is
	// where it survives the port intact.
	//
	// Empty for any provider that supplies no per-leg action; such a leg is
	// treated as principal, so nothing is ever dropped for want of this field.
	Action string

	// AssetID is the registry UUID this leg's (chain, contract) resolves to —
	// the identity the ledger records (issue #59, decision #35).
	//
	// It is populated by resolveAssetIdentities, which runs once over the whole
	// transaction before any builder reads it, and it is the ONLY asset identity
	// the ledger accepts from here on. AssetSymbol travels alongside it as
	// display metadata and is no longer an identifier: two contracts sharing a
	// ticker are two UUIDs, and one coin on two chains is likewise two.
	//
	// uuid.Nil means unresolved, which is a bug rather than a state — the
	// resolve fails the transaction rather than emitting a leg with no identity,
	// so nothing downstream has to decide what an identity-less leg means.
	AssetID uuid.UUID
}

// DecodedFee represents the gas fee for a decoded transaction
type DecodedFee struct {
	AssetSymbol string
	AssetName   string   // Human-readable name, empty if unknown
	Amount      *big.Int // Amount in base units (never nil)
	Decimals    int
	USDPrice    *big.Int // USD price scaled by 1e8, nil if unavailable
	IconURL     string   // Token icon URL, empty if unavailable

	// AssetID is the registry UUID of the coin the fee was paid in, resolved on
	// (chain, 'native') — gas is always paid in the chain's native coin. Set by
	// resolveAssetIdentities alongside every transfer leg, for the same reason:
	// gas moves value and therefore needs a real identity in the ledger, not a
	// ticker that happens to read "ETH" on a chain where it is not.
	AssetID uuid.UUID
}

// TransactionDataProvider fetches decoded transactions from an external API.
// It is chain-aware: the caller (Collector) owns the fan-out loop over a
// wallet's chains and invokes the provider once per chain.
//
// Results must be ordered oldest→newest and `since` must be treated as an
// INCLUSIVE lower bound (issue #29), so a transaction mined exactly at `since`
// is returned. The collector re-sorts defensively and relies on idempotent
// storage to absorb the duplicate at the boundary.
//
// StreamTransactions delivers history page by page so the collector can persist
// incrementally. That is what makes an interrupted deep sync resumable: a sync
// killed midway through a long backfill (context cancellation, exhausted
// retries, a 5xx on page 40 of 200) keeps everything already collected and
// resumes forward from its high-water mark, instead of discarding the whole
// fetch and restarting — which for a deep wallet may never converge.
//
// onPage is invoked once per page, oldest page first, and only with non-empty
// pages. If onPage returns an error, streaming stops and that error is returned.
//
// A provider that cannot page can satisfy this by invoking onPage once with the
// whole result.
type TransactionDataProvider interface {
	GetTransactions(ctx context.Context, address, chain string, since time.Time) ([]DecodedTransaction, error)

	StreamTransactions(
		ctx context.Context,
		address, chain string,
		since time.Time,
		onPage func([]DecodedTransaction) error,
	) error
}

// LPPositionService manages LP position lifecycle
type LPPositionService interface {
	FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, chainID, protocol, nftTokenID, contractAddress string, token0, token1 lpposition.TokenInfo, openedAt time.Time) (*lpposition.LPPosition, error)
	FindOpenByTokenPair(ctx context.Context, walletID uuid.UUID, chainID, protocol string, token0, token1 uuid.UUID) (*lpposition.LPPosition, error)
	RecordDeposit(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
	RecordWithdraw(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
	RecordClaimFees(ctx context.Context, positionID uuid.UUID, token0Amt, token1Amt, usdValue *big.Int) error
}

// LendingPositionService manages lending position lifecycle
type LendingPositionService interface {
	FindOrCreate(ctx context.Context, userID, walletID uuid.UUID, protocol, chainID string, openedAt time.Time) (*lendingposition.LendingPosition, error)
	RecordSupply(ctx context.Context, positionID uuid.UUID, assetID uuid.UUID, amount, usdValue *big.Int) error
	RecordWithdraw(ctx context.Context, positionID uuid.UUID, assetID uuid.UUID, amount, usdValue *big.Int) error
	RecordBorrow(ctx context.Context, positionID uuid.UUID, assetID uuid.UUID, amount, usdValue *big.Int) error
	RecordRepay(ctx context.Context, positionID uuid.UUID, assetID uuid.UUID, amount, usdValue *big.Int) error
	RecordClaim(ctx context.Context, positionID uuid.UUID, usdValue *big.Int) error
}

// AssetService defines asset operations for sync
type AssetService interface {
	// GetPriceBySymbol returns the current USD price for an asset by symbol (scaled by 10^8)
	// Returns nil if price unavailable (graceful degradation)
	GetPriceBySymbol(ctx context.Context, symbol string) (*big.Int, error)
}
