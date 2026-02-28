package sync

import "context"

// ZerionAssetRepository defines data access for Zerion-discovered asset metadata
type ZerionAssetRepository interface {
	// Upsert inserts or updates a zerion asset (ON CONFLICT symbol,chain_id DO UPDATE)
	Upsert(ctx context.Context, asset *ZerionAsset) error

	// GetBySymbol returns a zerion asset by symbol and chain_id.
	// If chainID is empty, returns the first match for any chain.
	GetBySymbol(ctx context.Context, symbol, chainID string) (*ZerionAsset, error)

	// GetAllBySymbol returns all zerion assets matching a symbol (across all chains)
	GetAllBySymbol(ctx context.Context, symbol string) ([]ZerionAsset, error)
}
