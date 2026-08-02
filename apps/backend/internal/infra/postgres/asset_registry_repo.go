package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/assetregistry"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Compile-time checks. The registry is the one store of asset identity after
// #59, so it serves the sync-side resolve, the price-side lookup and the
// presentation read behind the /assets endpoints.
var (
	_ sync.AssetRegistry   = (*AssetRegistryRepository)(nil)
	_ price.AssetLookup    = (*AssetRegistryRepository)(nil)
	_ assetregistry.Reader = (*AssetRegistryRepository)(nil)
)

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

	// A native coin is stamped with its chain's CoinGecko slug at creation
	// (issue #59, decision #39). This is the third and last of the three gates
	// that kept native coins unpriced: the price providers need an identifier
	// they can quote, and neither can derive one from the `native` sentinel —
	// DefiLlama is keyed on (chain, contract) and has no native key at all,
	// CoinGecko needs a coin slug. Without this the largest position in most
	// wallets, and every gas fee, would price at zero forever.
	//
	// Tokens are left NULL: their quote comes from (chain, contract), which the
	// row already carries. Writing a slug for them would be a second, weaker
	// identity competing with the contract.
	var coinGeckoID *string
	if key.IsNative() {
		if id, ok := sync.NativeCoinGeckoID(key.Chain); ok {
			coinGeckoID = &id
		}
	}

	const insertQuery = `
		INSERT INTO asset_registry (chain, contract, symbol, name, decimals, coingecko_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chain, contract) DO NOTHING
	`
	if _, err := r.pool.Exec(ctx, insertQuery,
		key.Chain, key.Contract, symbol, name, decimals, coinGeckoID,
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

// GetAsset returns the pricing identity for a registry UUID, satisfying
// price.AssetLookup (#59).
//
// The price pipeline needs three addressing schemes off one row: the UUID it
// caches and records under, the CoinGecko slug, and the (chain, contract) pair.
// All three live on the registry row, so this is a single-row read with no join
// — it replaces the `assets`-table lookup the backfill worker used before, whose
// id space was a different identity altogether.
//
// coingecko_id is nullable in the registry (an on-chain token need not be
// listed), and it is flattened to the empty string here rather than surfaced as
// a pointer. CoinGeckoProvider already treats "" as "I cannot address this" and
// answers ErrNotFound, so a NULL and an unset slug mean the same thing to every
// caller; a *string would only add a nil check at each one.
func (r *AssetRegistryRepository) GetAsset(ctx context.Context, id uuid.UUID) (*price.Asset, error) {
	const query = `
		SELECT id, chain, contract, COALESCE(coingecko_id, '')
		FROM asset_registry
		WHERE id = $1
	`
	var out price.Asset
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&out.ID, &out.Chain, &out.Contract, &out.CoinGeckoID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("asset registry row %s not found", id)
		}
		return nil, fmt.Errorf("failed to read asset registry row %s: %w", id, err)
	}
	return &out, nil
}

// registryRowColumns is the shared projection of every presentation read below,
// so the scan order cannot drift between queries.
const registryRowColumns = `id, chain, contract, symbol, name, decimals, COALESCE(coingecko_id, '')`

func scanRegistryRow(row pgx.Row) (*assetregistry.Asset, error) {
	var out assetregistry.Asset
	if err := row.Scan(&out.ID, &out.Chain, &out.Contract, &out.Symbol, &out.Name, &out.Decimals, &out.CoinGeckoID); err != nil {
		return nil, err
	}
	return &out, nil
}

func scanRegistryRows(rows pgx.Rows) ([]assetregistry.Asset, error) {
	defer rows.Close()
	out := make([]assetregistry.Asset, 0)
	for rows.Next() {
		r, err := scanRegistryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Get returns one registry row by its UUID, satisfying assetregistry.Reader.
func (r *AssetRegistryRepository) Get(ctx context.Context, id uuid.UUID) (*assetregistry.Asset, error) {
	query := `SELECT ` + registryRowColumns + ` FROM asset_registry WHERE id = $1`
	out, err := scanRegistryRow(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", assetregistry.ErrNotFound, id)
		}
		return nil, fmt.Errorf("failed to read asset registry row %s: %w", id, err)
	}
	return out, nil
}

// List returns registry rows filtered by symbol and/or chain.
//
// An empty filter half is "don't filter on it", which lets the one query serve
// all four combinations the /assets endpoint accepts. Symbol matching goes
// through UPPER() so it hits idx_asset_registry_symbol, the index 000034 kept
// alive specifically for this lookup.
//
// There is no is_active filter and no "all active assets" mode: the registry
// has no such column, and a row in it is an identity someone actually held
// rather than a catalogue entry (#59). An unfiltered call therefore lists the
// whole registry, bounded by limit.
func (r *AssetRegistryRepository) List(ctx context.Context, symbol, chain string, limit int) ([]assetregistry.Asset, error) {
	query := `
		SELECT ` + registryRowColumns + `
		FROM asset_registry
		WHERE ($1 = '' OR UPPER(symbol) = UPPER($1))
		  AND ($2 = '' OR chain = LOWER($2))
		ORDER BY symbol, chain, contract
		LIMIT $3
	`
	rows, err := r.pool.Query(ctx, query, symbol, chain, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list asset registry rows: %w", err)
	}
	out, err := scanRegistryRows(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan asset registry rows: %w", err)
	}
	return out, nil
}

// Search matches a free-text query against symbol and name.
//
// The registry ONLY — there is no CoinGecko fallback. The old
// SearchAssetsWithFallback queried an external provider and INSERTED whatever
// came back into the `assets` table, which is how a catalogue entry nobody held
// became an asset identity. Under the (chain, contract) registry that is not
// even expressible: CoinGecko answers with a coin slug, not a chain-scoped
// contract, so there is no key to insert under. Reinstating provider-backed
// discovery is #42's call, and it needs a shape that keeps discovery results
// out of the identity store.
//
// An exact symbol hit ranks above a prefix hit, which ranks above a substring
// hit, so searching "ETH" does not bury the ticker under every name containing
// those letters.
func (r *AssetRegistryRepository) Search(ctx context.Context, q string, limit int) ([]assetregistry.Asset, error) {
	// The pattern is passed as a bound parameter, and LIKE metacharacters in
	// user input are escaped so a query of "100%" cannot widen the match.
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	query := `
		SELECT ` + registryRowColumns + `
		FROM asset_registry
		WHERE UPPER(symbol) = UPPER($1)
		   OR symbol ILIKE $2 ESCAPE '\'
		   OR name   ILIKE $2 ESCAPE '\'
		ORDER BY
			CASE
				WHEN UPPER(symbol) = UPPER($1) THEN 0
				WHEN symbol ILIKE $3 ESCAPE '\' THEN 1
				ELSE 2
			END,
			symbol, chain, contract
		LIMIT $4
	`
	rows, err := r.pool.Query(ctx, query, q, "%"+escaped+"%", escaped+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search asset registry: %w", err)
	}
	out, err := scanRegistryRows(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan asset registry search results: %w", err)
	}
	return out, nil
}

// GetDecimals returns the decimals for an asset named by a CoinGecko slug or by
// its registry UUID, defaulting to 8 when it cannot be resolved.
//
// It serves the manual-transaction endpoint, whose request carries either a
// coingecko_id or an asset_id string and needs a scale to convert a
// human-readable amount into base units. Both spellings are accepted because
// that endpoint's contract accepts both; which one arrived is not knowable here.
//
// The 8 default is inherited verbatim from the asset.Service method this
// replaces (#59), so an unresolvable asset behaves exactly as before rather than
// changing amounts under existing clients. It is a fallback for a
// presentation-layer conversion, not an identity decision — nothing here creates
// a ledger row.
func (r *AssetRegistryRepository) GetDecimals(ctx context.Context, assetRef string) (int, error) {
	const defaultDecimals = 8
	if assetRef == "" {
		return defaultDecimals, nil
	}

	// One query covering both spellings. The UUID cast is guarded by the
	// regex so a non-UUID reference (the CoinGecko case) cannot raise a cast
	// error — Postgres would otherwise evaluate the cast before the match.
	const query = `
		SELECT decimals
		FROM asset_registry
		WHERE coingecko_id = $1
		   OR ($1 ~ '^[0-9a-fA-F-]{36}$' AND id = $1::uuid)
		LIMIT 1
	`
	var decimals int
	if err := r.pool.QueryRow(ctx, query, assetRef).Scan(&decimals); err != nil {
		return defaultDecimals, nil //nolint:nilerr // absent asset falls back, as before
	}
	return decimals, nil
}
