package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/platform/price"
)

// PriceRepository writes price_history.
//
// It used to also READ price_history in every shape asset.Service needed —
// current, at-a-time, hourly/daily/weekly history, OHLCV, recent. All of those
// served the `/assets/{id}/price` and `/assets/{id}/history` endpoints that went
// with the `assets` table in #59, so they are gone with their only caller. The
// reads that survive live in PriceReader, which is the priority-ordered reader
// the price resolver actually uses.
type PriceRepository struct {
	pool *pgxpool.Pool
}

// NewPriceRepository creates a new PostgreSQL price repository
func NewPriceRepository(pool *pgxpool.Pool) *PriceRepository {
	return &PriceRepository{pool: pool}
}

// RecordPricePoint writes one price_history row from the price package's own
// PricePoint, satisfying price.PriceRecorder (#59).
//
// It exists alongside RecordPrice because the two callers no longer share a
// type: the backfill worker speaks price.PricePoint, keyed on an asset_registry
// UUID, while RecordPrice still serves the asset.Service reads. Both write the
// same table and the same conflict target, so the row semantics are identical —
// only the struct differs.
//
// Volume and market cap are not written: a backfilled point is a price at a
// moment, and the resolver's providers return no volume alongside it. Leaving
// the columns NULL says "not measured", which is the truth; writing zeros would
// be indistinguishable from a genuine zero-volume observation.
func (r *PriceRepository) RecordPricePoint(ctx context.Context, p *price.PricePoint) error {
	if p == nil || p.PriceUSD == nil {
		return fmt.Errorf("invalid price: nil price point")
	}
	if p.AssetID == uuid.Nil {
		return fmt.Errorf("invalid price: missing asset id")
	}

	const query = `
		INSERT INTO price_history (time, asset_id, price_usd, source)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (asset_id, time) DO UPDATE SET
			price_usd = EXCLUDED.price_usd,
			source = EXCLUDED.source
	`
	if _, err := r.pool.Exec(ctx, query,
		p.Time, p.AssetID, p.PriceUSD.String(), string(p.Source),
	); err != nil {
		return fmt.Errorf("failed to record price point: %w", err)
	}
	return nil
}
