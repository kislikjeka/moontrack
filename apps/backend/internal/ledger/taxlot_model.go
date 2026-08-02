package ledger

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// PriceStatus describes the cost-basis resolution state of a tax lot.
type PriceStatus string

const (
	// PriceStatusResolved means the lot has a known auto_cost_basis_per_unit.
	PriceStatusResolved PriceStatus = "resolved"
	// PriceStatusPending means no price was available at acquisition time;
	// a backfill job will resolve it asynchronously.
	PriceStatusPending PriceStatus = "pending"
	// PriceStatusUnpriceable means all resolution attempts have been exhausted.
	PriceStatusUnpriceable PriceStatus = "unpriceable"
)

// CostBasisSource describes how the cost basis was determined
type CostBasisSource string

const (
	CostBasisSwapPrice            CostBasisSource = "swap_price"
	CostBasisFMVAtTransfer        CostBasisSource = "fmv_at_transfer"
	CostBasisLinkedTransfer       CostBasisSource = "linked_transfer"
	CostBasisGenesisApproximation CostBasisSource = "genesis_approximation"
	CostBasisLendingCarryOver     CostBasisSource = "lending_carry_over"
)

// DisposalType describes how the asset was disposed of
type DisposalType string

const (
	DisposalTypeSale             DisposalType = "sale"
	DisposalTypeInternalTransfer DisposalType = "internal_transfer"
	DisposalTypeGasFee           DisposalType = "gas_fee"
	DisposalTypeLendingTransfer  DisposalType = "lending_transfer"
)

// TaxLot represents a batch of asset acquired in a single transaction.
// Each acquisition on a CRYPTO_WALLET account creates one tax lot.
type TaxLot struct {
	ID            uuid.UUID
	TransactionID uuid.UUID
	AccountID     uuid.UUID
	// Asset is the asset registry UUID (issue #59). It used to be a bare ticker
	// with NO chain, so the chain could only be recovered by joining through
	// accounts — which is why the pending-lot and disposal queries had to join
	// at all. The UUID carries (chain, contract) by construction.
	Asset             uuid.UUID
	QuantityAcquired  *big.Int
	QuantityRemaining *big.Int
	AcquiredAt        time.Time
	// AutoCostBasisPerUnit is the USD rate scaled 10^8. It may be nil for
	// lots in PriceStatusPending state where the price has not been resolved yet.
	AutoCostBasisPerUnit     *big.Int
	AutoCostBasisSource      CostBasisSource
	OverrideCostBasisPerUnit *big.Int   // nullable
	OverrideReason           *string    // nullable
	OverrideAt               *time.Time // nullable
	LinkedSourceLotID        *uuid.UUID // nullable — for internal transfers
	CreatedAt                time.Time
	ChainID                  string // not persisted — populated at runtime by service layer

	// Price resolution state — backed by price_status, price_resolution_attempts,
	// and price_next_retry_at columns added in migration 000025.
	PriceStatus             PriceStatus
	PriceResolutionAttempts int
	PriceNextRetryAt        *time.Time
}

// EffectiveCostBasisPerUnit returns the cost basis to use for PnL calculations.
// Priority: override > auto. Returns nil when the lot is pending and has
// neither an auto nor override cost basis yet — callers must handle nil.
func (l *TaxLot) EffectiveCostBasisPerUnit() *big.Int {
	if l.OverrideCostBasisPerUnit != nil {
		return l.OverrideCostBasisPerUnit
	}
	return l.AutoCostBasisPerUnit // may be nil for pending lots
}

// IsOpen returns true if the lot still has remaining quantity.
func (l *TaxLot) IsOpen() bool {
	return l.QuantityRemaining != nil && l.QuantityRemaining.Sign() > 0
}

// ProceedsStatus describes the resolution state of a disposal's proceeds_per_unit.
type ProceedsStatus string

const (
	// ProceedsStatusResolved means proceeds_per_unit is a known USD rate.
	ProceedsStatusResolved ProceedsStatus = "resolved"
	// ProceedsStatusPending means no price was available at disposal time;
	// the price-backfill worker will fill it in asynchronously.
	ProceedsStatusPending ProceedsStatus = "pending"
	// ProceedsStatusUnpriceable means all attempts to resolve the price
	// have been exhausted.
	ProceedsStatusUnpriceable ProceedsStatus = "unpriceable"
)

// LotDisposal records the consumption of a tax lot during a disposal event.
type LotDisposal struct {
	ID               uuid.UUID
	TransactionID    uuid.UUID
	LotID            uuid.UUID
	QuantityDisposed *big.Int
	// ProceedsPerUnit is the USD rate scaled 10^8. It may be nil while the
	// disposal is in ProceedsStatusPending — callers performing PnL must
	// check ProceedsStatus and skip unresolved disposals.
	ProceedsPerUnit *big.Int
	ProceedsStatus  ProceedsStatus
	DisposalType    DisposalType
	DisposedAt      time.Time
	CreatedAt       time.Time
}

// PositionWAC represents the weighted average cost for an (account, asset) position.
type PositionWAC struct {
	AccountID       uuid.UUID
	Asset           uuid.UUID
	TotalQuantity   *big.Int
	WeightedAvgCost *big.Int // USD scaled 10^8
}

// LotOverrideHistory records changes to a lot's cost basis override.
type LotOverrideHistory struct {
	ID                uuid.UUID
	LotID             uuid.UUID
	PreviousCostBasis *big.Int // nullable (nil if first override)
	NewCostBasis      *big.Int
	Reason            string
	ChangedAt         time.Time
}
