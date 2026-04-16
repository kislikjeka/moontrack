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
	GetOpenLotsFIFO(ctx context.Context, accountID uuid.UUID, asset string) ([]*TaxLot, error)
	UpdateLotRemaining(ctx context.Context, lotID uuid.UUID, newRemaining *big.Int) error
	GetLotsByAccount(ctx context.Context, accountID uuid.UUID, asset string) ([]*TaxLot, error)
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

	// ListPendingLotsByAssetAndTime returns all lots whose price_status is 'pending'
	// for the given asset symbol within the minute bucket containing at.
	ListPendingLotsByAssetAndTime(ctx context.Context, asset string, at time.Time) ([]*TaxLot, error)

	// ResolvePendingPrice sets auto_cost_basis_per_unit and transitions
	// price_status to 'resolved'. Only affects rows where price_status='pending'.
	ResolvePendingPrice(ctx context.Context, lotID uuid.UUID, autoCostBasisPerUnit *big.Int, autoSource CostBasisSource) error

	// MarkUnpriceable transitions price_status to 'unpriceable' for a pending lot.
	MarkUnpriceable(ctx context.Context, lotID uuid.UUID) error

	// IncrementAttempt bumps the attempts counter and sets the next-retry time
	// for a pending lot.
	IncrementAttempt(ctx context.Context, lotID uuid.UUID, attempts int, nextRetryAt time.Time) error
}
