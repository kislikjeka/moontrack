package ledger

import (
	"context"
	"math/big"
	"time"

	"github.com/kislikjeka/moontrack/pkg/logger"
)

// PriceResolvedHook is called by the backfill worker once it has resolved a
// historical price for a (asset, time) pair. It finds all pending lots that
// match the minute-bucket of at and transitions them to resolved.
//
// Downstream disposal recomputation is implicit: LotDisposal rows do not cache
// cost basis — they call EffectiveCostBasisPerUnit() on the source lot at read
// time. Resolving the lot is therefore sufficient.
type PriceResolvedHook func(ctx context.Context, asset string, at time.Time, priceUSDPerUnit *big.Int, source CostBasisSource) error

// NewPriceResolvedHook constructs a PriceResolvedHook backed by repo.
func NewPriceResolvedHook(repo TaxLotRepository, log *logger.Logger) PriceResolvedHook {
	hlog := log.WithField("component", "price_resolved_hook")
	return func(ctx context.Context, asset string, at time.Time, price *big.Int, source CostBasisSource) error {
		lots, err := repo.ListPendingLotsByAssetAndTime(ctx, asset, at)
		if err != nil {
			return err
		}
		for _, lot := range lots {
			if err := repo.ResolvePendingPrice(ctx, lot.ID, price, source); err != nil {
				return err
			}
			hlog.Info("resolved pending lot",
				"lot_id", lot.ID.String(), "asset", asset, "price", price.String())
		}
		return nil
	}
}
