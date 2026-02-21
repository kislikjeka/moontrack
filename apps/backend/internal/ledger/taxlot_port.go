package ledger

import (
	"context"
	"math/big"

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
}
