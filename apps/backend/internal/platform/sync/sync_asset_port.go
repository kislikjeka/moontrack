package sync

import "context"

// SyncAssetRepository defines data access for sync-discovered asset metadata
type SyncAssetRepository interface {
	// Upsert inserts or updates a sync asset (ON CONFLICT symbol,chain_id DO UPDATE)
	Upsert(ctx context.Context, asset *SyncAsset) error

	// GetBySymbol returns a sync asset by symbol and chain_id.
	// If chainID is empty, returns the first match for any chain.
	GetBySymbol(ctx context.Context, symbol, chainID string) (*SyncAsset, error)

	// GetAllBySymbol returns all sync assets matching a symbol (across all chains)
	GetAllBySymbol(ctx context.Context, symbol string) ([]SyncAsset, error)
}
