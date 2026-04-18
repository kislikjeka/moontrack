package ledger

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// PriceResolvedHook is called by the backfill worker once it has resolved a
// historical price for a (assetID, time) pair. It finds all pending lots that
// match the minute-bucket of at and transitions them to resolved.
//
// assetID is the asset UUID, not the symbol. Using the UUID prevents
// cross-chain collisions (e.g. USDT on Ethereum vs USDT on BNB).
//
// Downstream disposal recomputation is implicit: LotDisposal rows do not cache
// cost basis — they call EffectiveCostBasisPerUnit() on the source lot at read
// time. Resolving the lot is therefore sufficient.
type PriceResolvedHook func(ctx context.Context, assetID uuid.UUID, at time.Time, priceUSDPerUnit *big.Int, source CostBasisSource) error

// NewPriceResolvedHook constructs a PriceResolvedHook backed by repo.
//
// Resolution covers two distinct states:
//  1. Pending tax lots — acquisitions recorded without a USD rate. Resolving
//     the lot fixes its cost basis.
//  2. Pending disposals — outflows recorded without a USD rate. Prior to
//     migration 000026 these were frozen at proceeds_per_unit=0; now they
//     carry proceeds_status='pending' and are filled in here.
func NewPriceResolvedHook(repo TaxLotRepository, log *logger.Logger) PriceResolvedHook {
	hlog := log.WithField("component", "price_resolved_hook")
	return func(ctx context.Context, assetID uuid.UUID, at time.Time, price *big.Int, source CostBasisSource) error {
		// 1. Resolve pending tax lots (cost basis side).
		lots, err := repo.ListPendingLotsByAssetIDAndTime(ctx, assetID, at)
		if err != nil {
			return err
		}
		for _, lot := range lots {
			if err := repo.ResolvePendingPrice(ctx, lot.ID, price, source); err != nil {
				// A concurrent invocation may have already flipped the lot to resolved,
				// in which case the repo's price_status='pending' guard returns ErrLotNotFound.
				// Treat that as a successful no-op — the other caller won.
				if errors.Is(err, ErrLotNotFound) {
					continue
				}
				return err
			}
			hlog.Info("resolved pending lot",
				"lot_id", lot.ID.String(), "asset_id", assetID.String(), "price", price.String())
		}

		// 2. Resolve pending disposals (proceeds side).
		n, err := repo.ResolvePendingDisposals(ctx, assetID, at, price)
		if err != nil {
			return err
		}
		if n > 0 {
			hlog.Info("resolved pending disposals",
				"count", n, "asset_id", assetID.String(), "price", price.String())
		}
		return nil
	}
}
