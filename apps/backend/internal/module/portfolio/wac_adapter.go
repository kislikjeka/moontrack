package portfolio

import (
	"context"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/platform/taxlot"
)

// TaxLotWACService is the subset of taxlot.Service needed by the WAC adapter.
type TaxLotWACService interface {
	GetWAC(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID) ([]taxlot.WACPosition, error)
}

// WACAdapter adapts taxlot.Service to the portfolio.WACProvider interface.
type WACAdapter struct {
	svc TaxLotWACService
}

// NewWACAdapter creates a new WAC adapter.
func NewWACAdapter(svc TaxLotWACService) *WACAdapter {
	return &WACAdapter{svc: svc}
}

// GetWAC returns WAC positions mapped to the portfolio domain type.
func (a *WACAdapter) GetWAC(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID) ([]WACPosition, error) {
	raw, err := a.svc.GetWAC(ctx, userID, walletID)
	if err != nil {
		return nil, err
	}

	result := make([]WACPosition, len(raw))
	for i, p := range raw {
		result[i] = WACPosition{
			WalletID:        p.WalletID,
			AccountID:       p.AccountID,
			ChainID:         p.ChainID,
			Asset:           p.Asset,
			TotalQuantity:   p.TotalQuantity,
			WeightedAvgCost: p.WeightedAvgCost,
			IsAggregated:    p.AccountID == uuid.Nil,
		}
	}

	return result, nil
}
