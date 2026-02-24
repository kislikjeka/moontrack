package ledger

import (
	"math/big"
	"time"

	"github.com/google/uuid"
)

// CostBasisSource describes how the cost basis was determined
type CostBasisSource string

const (
	CostBasisSwapPrice            CostBasisSource = "swap_price"
	CostBasisFMVAtTransfer        CostBasisSource = "fmv_at_transfer"
	CostBasisLinkedTransfer       CostBasisSource = "linked_transfer"
	CostBasisGenesisApproximation CostBasisSource = "genesis_approximation"
)

// DisposalType describes how the asset was disposed of
type DisposalType string

const (
	DisposalTypeSale             DisposalType = "sale"
	DisposalTypeInternalTransfer DisposalType = "internal_transfer"
	DisposalTypeGasFee           DisposalType = "gas_fee"
)

// TaxLot represents a batch of asset acquired in a single transaction.
// Each acquisition on a CRYPTO_WALLET account creates one tax lot.
type TaxLot struct {
	ID                       uuid.UUID
	TransactionID            uuid.UUID
	AccountID                uuid.UUID
	Asset                    string
	QuantityAcquired         *big.Int
	QuantityRemaining        *big.Int
	AcquiredAt               time.Time
	AutoCostBasisPerUnit     *big.Int        // USD rate scaled 10^8
	AutoCostBasisSource      CostBasisSource
	OverrideCostBasisPerUnit *big.Int   // nullable
	OverrideReason           *string    // nullable
	OverrideAt               *time.Time // nullable
	LinkedSourceLotID        *uuid.UUID // nullable — for internal transfers
	CreatedAt                time.Time
	ChainID                  string     // not persisted — populated at runtime by service layer
}

// EffectiveCostBasisPerUnit returns the cost basis to use for PnL calculations.
// Priority: override > auto.
func (l *TaxLot) EffectiveCostBasisPerUnit() *big.Int {
	if l.OverrideCostBasisPerUnit != nil {
		return l.OverrideCostBasisPerUnit
	}
	return l.AutoCostBasisPerUnit
}

// IsOpen returns true if the lot still has remaining quantity.
func (l *TaxLot) IsOpen() bool {
	return l.QuantityRemaining != nil && l.QuantityRemaining.Sign() > 0
}

// LotDisposal records the consumption of a tax lot during a disposal event.
type LotDisposal struct {
	ID               uuid.UUID
	TransactionID    uuid.UUID
	LotID            uuid.UUID
	QuantityDisposed *big.Int
	ProceedsPerUnit  *big.Int // USD rate scaled 10^8
	DisposalType     DisposalType
	DisposedAt       time.Time
	CreatedAt        time.Time
}

// PositionWAC represents the weighted average cost for an (account, asset) position.
type PositionWAC struct {
	AccountID       uuid.UUID
	Asset           string
	TotalQuantity   *big.Int
	WeightedAvgCost *big.Int // USD scaled 10^8
}

// LotOverrideHistory records changes to a lot's cost basis override.
type LotOverrideHistory struct {
	ID               uuid.UUID
	LotID            uuid.UUID
	PreviousCostBasis *big.Int // nullable (nil if first override)
	NewCostBasis     *big.Int
	Reason           string
	ChangedAt        time.Time
}
