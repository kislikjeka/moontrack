package portfolio

import (
	"context"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/assetregistry"
)

// AssetRegistryLookup implements AssetLookup against the asset registry (#59).
//
// It exists because the portfolio only ever holds a registry UUID now: the
// ledger balances it sums carry nothing else, so both the ticker to display and
// the decimals to scale by have to be read from the row that id names. Reading
// them together is the point — they are two columns of one row, and the two
// separate lookups they replace (a ticker table for decimals, a symbol string
// for display) are exactly how the two used to disagree.
//
// The dependency is assetregistry.Reader rather than the postgres repository so
// this stays inside the layering rule (module → platform), and so a test can
// supply a map instead of a database.
type AssetRegistryLookup struct {
	reader assetregistry.Reader
}

// NewAssetRegistryLookup creates a registry-backed asset lookup.
func NewAssetRegistryLookup(reader assetregistry.Reader) *AssetRegistryLookup {
	return &AssetRegistryLookup{reader: reader}
}

// Describe returns the symbol and decimals for a registry id.
//
// A miss returns ok=false rather than a guess. The caller then renders no
// symbol at all, which is the honest answer: an asset the registry does not
// know has no ticker this service can supply, and inventing one would put a
// label on a holding that nothing backs.
func (l *AssetRegistryLookup) Describe(ctx context.Context, assetID uuid.UUID) (string, int, bool) {
	if l.reader == nil || assetID == uuid.Nil {
		return "", 0, false
	}
	asset, err := l.reader.Get(ctx, assetID)
	if err != nil || asset == nil {
		return "", 0, false
	}
	return asset.Symbol, asset.Decimals, true
}
