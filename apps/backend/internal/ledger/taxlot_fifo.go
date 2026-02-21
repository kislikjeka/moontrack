package ledger

import (
	"context"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// DisposeFIFO consumes tax lots in FIFO order for a disposal event.
// It creates LotDisposal records and updates lot remaining quantities.
// Returns the list of disposals created, or ErrInsufficientLots if there
// aren't enough lots to cover the full quantity (partial disposals still happen).
func DisposeFIFO(
	ctx context.Context,
	repo TaxLotRepository,
	accountID uuid.UUID,
	asset string,
	quantity *big.Int,
	proceedsPerUnit *big.Int,
	disposalType DisposalType,
	transactionID uuid.UUID,
	disposedAt time.Time,
) ([]*LotDisposal, error) {
	// Zero or nil quantity -> no-op
	if quantity == nil || quantity.Sign() <= 0 {
		return nil, nil
	}

	remaining := new(big.Int).Set(quantity)

	lots, err := repo.GetOpenLotsFIFO(ctx, accountID, asset)
	if err != nil {
		return nil, err
	}

	var disposals []*LotDisposal

	for _, lot := range lots {
		if remaining.Sign() <= 0 {
			break
		}

		// disposeQty = min(lot.QuantityRemaining, remaining)
		disposeQty := new(big.Int)
		if lot.QuantityRemaining.Cmp(remaining) <= 0 {
			disposeQty.Set(lot.QuantityRemaining)
		} else {
			disposeQty.Set(remaining)
		}

		disposal := &LotDisposal{
			ID:               uuid.New(),
			TransactionID:    transactionID,
			LotID:            lot.ID,
			QuantityDisposed: disposeQty,
			ProceedsPerUnit:  new(big.Int).Set(proceedsPerUnit),
			DisposalType:     disposalType,
			DisposedAt:       disposedAt,
			CreatedAt:        time.Now(),
		}

		if err := repo.CreateDisposal(ctx, disposal); err != nil {
			return disposals, err
		}

		newRemaining := new(big.Int).Sub(lot.QuantityRemaining, disposeQty)
		if err := repo.UpdateLotRemaining(ctx, lot.ID, newRemaining); err != nil {
			return disposals, err
		}

		disposals = append(disposals, disposal)
		remaining.Sub(remaining, disposeQty)
	}

	if remaining.Sign() > 0 {
		return disposals, ErrInsufficientLots
	}

	return disposals, nil
}
