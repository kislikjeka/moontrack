package ledger

import (
	"context"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// TaxLotRepository defines persistence operations for tax lots.
// Implementations MUST participate in the ledger DB transaction (via context)
// to guarantee atomicity with entry/balance writes.
type TaxLotRepository interface {
	// Lot CRUD
	CreateTaxLot(ctx context.Context, lot *TaxLot) error
	GetTaxLot(ctx context.Context, id uuid.UUID) (*TaxLot, error)
	GetTaxLotForUpdate(ctx context.Context, id uuid.UUID) (*TaxLot, error)
	GetOpenLotsFIFO(ctx context.Context, accountID uuid.UUID, asset uuid.UUID) ([]*TaxLot, error)
	UpdateLotRemaining(ctx context.Context, lotID uuid.UUID, newRemaining *big.Int) error
	GetLotsByAccount(ctx context.Context, accountID uuid.UUID, asset uuid.UUID) ([]*TaxLot, error)
	GetLotsByTransaction(ctx context.Context, txID uuid.UUID) ([]*TaxLot, error)

	// Disposal CRUD
	CreateDisposal(ctx context.Context, disposal *LotDisposal) error
	GetDisposalsByTransaction(ctx context.Context, txID uuid.UUID) ([]*LotDisposal, error)
	GetDisposalsByLot(ctx context.Context, lotID uuid.UUID) ([]*LotDisposal, error)

	// Override management
	UpdateOverride(ctx context.Context, lotID uuid.UUID, costBasis *big.Int, reason string) error
	ClearOverride(ctx context.Context, lotID uuid.UUID) error
	CreateOverrideHistory(ctx context.Context, history *LotOverrideHistory) error
	GetOverrideHistory(ctx context.Context, lotID uuid.UUID) ([]*LotOverrideHistory, error)

	// WAC (weighted average cost)
	RefreshWAC(ctx context.Context) error
	GetWAC(ctx context.Context, accountIDs []uuid.UUID) ([]*PositionWAC, error)

	// Pending-price resolution methods (migration 000025)

	// ListPendingLotsByAssetAndTime returns all lots whose price_status is
	// 'pending' for the given asset within the minute bucket containing at.
	//
	// THIS METHOD REPLACES A PAIR (issue #59). There used to be two: one keyed
	// on a bare symbol with no chain at all — `WHERE asset = $1`, which matched
	// every chain's lots sharing a ticker and is the fourth of the four places
	// the epic called out — and one keyed on a UUID that had to resolve back to
	// (symbol, chain_id) and JOIN accounts to scope itself correctly.
	//
	// Under registry identity both collapse into this one. The asset UUID already
	// names exactly one (chain, contract), so there is nothing left to
	// disambiguate and no join left to get wrong: the unscoped variant cannot be
	// written any more, because there is no longer a key that spans chains.
	ListPendingLotsByAssetAndTime(ctx context.Context, assetID uuid.UUID, at time.Time) ([]*TaxLot, error)

	// ResolvePendingPrice sets auto_cost_basis_per_unit and transitions
	// price_status to 'resolved'. Only affects rows where price_status='pending'.
	ResolvePendingPrice(ctx context.Context, lotID uuid.UUID, autoCostBasisPerUnit *big.Int, autoSource CostBasisSource) error

	// ResolvePendingDisposals sets proceeds_per_unit and transitions
	// proceeds_status to 'resolved' for every disposal in the given minute
	// bucket whose lot is scoped to the asset identified by assetID. Only
	// affects rows with proceeds_status='pending'. Returns the number of
	// rows updated so callers can report resolution progress.
	//
	// NOTE: This variant is global (no user_id filter) and is intended only
	// for the backfill hook where a resolved price applies across all tenants
	// for the same (asset_id, minute_bucket). Per-user flows (e.g. manual
	// price overrides) MUST use ResolvePendingDisposalsForUser to avoid
	// cross-tenant contamination.
	ResolvePendingDisposals(ctx context.Context, assetID uuid.UUID, at time.Time, proceedsPerUnit *big.Int) (int64, error)

	// ResolvePendingDisposalsForUser is the user-scoped variant of
	// ResolvePendingDisposals. It only touches disposals whose owning account
	// belongs to userID, preventing one tenant's manual price from mutating
	// another tenant's pending disposals that happen to share the same
	// (asset_id, minute_bucket).
	ResolvePendingDisposalsForUser(ctx context.Context, userID uuid.UUID, assetID uuid.UUID, at time.Time, proceedsPerUnit *big.Int) (int64, error)

	// MarkUnpriceable transitions price_status to 'unpriceable' for a pending lot.
	MarkUnpriceable(ctx context.Context, lotID uuid.UUID) error

	// MarkResolved transitions price_status to 'resolved' for any lot
	// (pending or unpriceable). Used when a manual price is applied by the user.
	MarkResolved(ctx context.Context, lotID uuid.UUID) error

	// IncrementAttempt bumps the attempts counter and sets the next-retry time
	// for a pending lot.
	IncrementAttempt(ctx context.Context, lotID uuid.UUID, attempts int, nextRetryAt time.Time) error

	// CountLotsByPriceStatus returns the number of lots in 'pending' and 'unpriceable'
	// price_status for the given user. Filters by user_id via accounts JOIN to
	// preserve multi-tenant isolation.
	CountLotsByPriceStatus(ctx context.Context, userID uuid.UUID) (pending, unpriceable int, err error)
}
