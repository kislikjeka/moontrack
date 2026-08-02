package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// Compile-time check that the registry can feed the decimal resolver.
var _ money.AssetDecimalSource = (*RegistryDecimalSource)(nil)

// RegistryDecimalSource answers "how many decimals does this ticker have"
// from asset_registry (#59).
//
// It replaces two sources that died with their tables: asset.DecimalSource over
// `assets` and sync.DecimalSource over `chain_assets`. Those were cascaded in
// that order, which was itself a symptom — two stores held decimals for the same
// asset and disagreed, and the cascade encoded which disagreement to believe.
// The registry is now the single place decimals live, so there is one source and
// nothing to arbitrate.
//
// WHY THIS IS STILL SYMBOL-KEYED, when the whole ticket exists to stop keying
// assets on symbols: money.DecimalResolver is asked to scale amounts for
// presentation (portfolio rows, transaction views) where the caller genuinely
// has only a ticker in hand. That is a display concern, not identity — nothing
// read here reaches the ledger, and no lot, entry or account is created from it.
// The identity path resolves (chain, contract) → UUID in sync and never consults
// this. A UUID-keyed decimals lookup belongs with the API contract work (#42),
// which is what gives those presentation surfaces a real asset id to ask with.
type RegistryDecimalSource struct {
	pool *pgxpool.Pool
}

// NewRegistryDecimalSource creates a decimal source backed by asset_registry.
func NewRegistryDecimalSource(pool *pgxpool.Pool) *RegistryDecimalSource {
	return &RegistryDecimalSource{pool: pool}
}

// GetDecimalsBySymbol returns decimals for a symbol, optionally scoped to a chain.
//
// Symbol is deliberately NOT unique in the registry — two contracts sharing a
// ticker on one chain is the case the registry was built to represent — so this
// lookup can legitimately match several rows. It answers only when every
// candidate agrees on decimals, and declines otherwise.
//
// Declining is the important half. Picking the first row would silently choose
// one token's scale for another token's amount, which misplaces a decimal point
// by orders of magnitude and looks like a balance, not like an error. Returning
// false instead lets the resolver fall through to its hardcoded table, and a
// wrong-but-loud default beats a wrong-and-plausible one. The DISTINCT plus the
// row count is what makes "they all agree" a property of the query rather than
// something the caller has to check.
func (s *RegistryDecimalSource) GetDecimalsBySymbol(ctx context.Context, symbol, chainID string) (int, bool) {
	if symbol == "" {
		return 0, false
	}

	// LIMIT 2 is all the disambiguation needed: one row means unanimous, two
	// means the candidates disagree and the answer is refused. Fetching the
	// whole set would only make a bigger version of the same yes/no.
	const query = `
		SELECT DISTINCT decimals
		FROM asset_registry
		WHERE UPPER(symbol) = UPPER($1)
		  AND ($2 = '' OR chain = LOWER($2))
		LIMIT 2
	`
	rows, err := s.pool.Query(ctx, query, symbol, chainID)
	if err != nil {
		return 0, false
	}
	defer rows.Close()

	var decimals int
	found := 0
	for rows.Next() {
		var d int
		if err := rows.Scan(&d); err != nil {
			return 0, false
		}
		decimals = d
		found++
	}
	if err := rows.Err(); err != nil {
		return 0, false
	}
	if found != 1 {
		// Zero candidates, or several that disagree — either way this source
		// has no answer it can stand behind.
		return 0, false
	}
	return decimals, true
}
