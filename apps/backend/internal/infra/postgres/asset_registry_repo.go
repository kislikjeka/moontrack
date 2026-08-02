package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Compile-time check that AssetRegistryRepository implements sync.AssetRegistry.
var _ sync.AssetRegistry = (*AssetRegistryRepository)(nil)

// AssetRegistryRepository is the asset registry backed by the asset_registry
// table, keyed on (chain, contract) (issue #56).
type AssetRegistryRepository struct {
	pool *pgxpool.Pool
}

// NewAssetRegistryRepository creates a PostgreSQL-backed asset registry.
func NewAssetRegistryRepository(pool *pgxpool.Pool) *AssetRegistryRepository {
	return &AssetRegistryRepository{pool: pool}
}

// ErrInvalidAssetKey is returned when an identity is missing a half. The
// registry's NOT NULL plus non-blank CHECK would reject the row anyway; failing
// here turns a constraint violation into a named error at the call site.
var ErrInvalidAssetKey = errors.New("asset key requires both chain and contract")

// Resolve returns the registry asset for the key, inserting it on first sight.
//
// Insert-then-select rather than select-then-insert: ON CONFLICT DO NOTHING
// makes the write idempotent under concurrency, so two syncs meeting the same
// asset for the first time converge on one row instead of racing between the
// check and the insert. The follow-up SELECT then reads whichever row won,
// which is also the path taken on every subsequent (already-known) sighting.
//
// Metadata is written only by the INSERT. A conflict deliberately updates
// nothing: re-reporting a known contract with different decimals must not
// retroactively invalidate conversions already made against the stored value.
// That silent overwrite is the live defect in chain_assets, whose upsert
// assigns decimals = EXCLUDED.decimals unconditionally under (symbol, chain_id)
// uniqueness.
func (r *AssetRegistryRepository) Resolve(ctx context.Context, key sync.AssetKey, symbol, name string, decimals int) (*sync.RegistryAsset, error) {
	if !key.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAssetKey, key.String())
	}

	const insertQuery = `
		INSERT INTO asset_registry (chain, contract, symbol, name, decimals)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (chain, contract) DO NOTHING
	`
	if _, err := r.pool.Exec(ctx, insertQuery,
		key.Chain, key.Contract, symbol, name, decimals,
	); err != nil {
		return nil, fmt.Errorf("failed to insert asset registry row for %s: %w", key.String(), err)
	}

	const selectQuery = `
		SELECT id, chain, contract, symbol, name, decimals
		FROM asset_registry
		WHERE chain = $1 AND contract = $2
	`
	var out sync.RegistryAsset
	err := r.pool.QueryRow(ctx, selectQuery, key.Chain, key.Contract).Scan(
		&out.ID, &out.Key.Chain, &out.Key.Contract, &out.Symbol, &out.Name, &out.Decimals,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The INSERT either created the row or found it already there, so
			// its absence now means something deleted it in between. Surface it
			// rather than returning a nil asset the caller might read as "no
			// price available".
			return nil, fmt.Errorf("asset registry row for %s vanished after insert", key.String())
		}
		return nil, fmt.Errorf("failed to read asset registry row for %s: %w", key.String(), err)
	}

	return &out, nil
}
