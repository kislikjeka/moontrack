// Package assetregistry is the read side of the asset registry: the full
// registry row as anything above the infra layer sees it.
//
// It exists because the registry row has three consumers with three different
// appetites, and #59 gives each the shape it actually uses rather than one wide
// type shared by all:
//
//   - sync.RegistryAsset — identity plus the metadata written on first sight.
//     No coingecko_id; the sync path never reads it.
//   - price.Asset — the UUID plus the two provider addressing schemes. No
//     symbol, name or decimals; a provider cannot be addressed by any of them.
//   - assetregistry.Asset (here) — everything, because the /assets endpoints
//     present the row to a human.
//
// The type lives in platform rather than in infra/postgres so the handler can
// name it without transport importing infra, which the layering rule
// (transport → module → platform → ledger ← infra) forbids. The postgres
// repository implements Reader; nobody above depends on the repository's own
// package.
package assetregistry

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound is returned when no registry row carries the requested id.
var ErrNotFound = errors.New("asset not found in registry")

// Asset is one asset_registry row.
//
// Contract carries the registry's spelling VERBATIM, including the `native`
// sentinel for a chain's native coin. It is never translated back to an empty
// string or a nil pointer on the way out: those were two of the four
// inconsistent spellings of nativeness that #59 removes, and reintroducing one
// at the presentation edge would put it straight back.
//
// CoinGeckoID is flattened from a nullable column to the empty string. A NULL
// slug and an unset slug mean the same thing to every consumer — "this asset is
// not addressable at CoinGecko" — so a pointer would only add a nil check.
type Asset struct {
	ID          uuid.UUID
	Chain       string
	Contract    string
	Symbol      string
	Name        string
	Decimals    int
	CoinGeckoID string
}

// Reader reads registry rows for presentation.
//
// There is deliberately no "list active assets": the registry has no is_active
// column, because a row in it is an identity someone actually held rather than
// a catalogue entry that can be switched off (#59). An unfiltered List returns
// the whole registry, bounded by limit.
type Reader interface {
	// Get returns the row with this id, or ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (*Asset, error)

	// List returns rows filtered by symbol and/or chain. An empty filter half
	// means "do not filter on it", so one call serves all four combinations
	// the /assets endpoint accepts.
	List(ctx context.Context, symbol, chain string, limit int) ([]Asset, error)

	// Search matches a free-text query against symbol and name, registry only.
	// There is no external-provider fallback — see the postgres implementation
	// for why it is not expressible against a (chain, contract) identity, and
	// #42 for where provider-backed discovery belongs.
	Search(ctx context.Context, q string, limit int) ([]Asset, error)
}
