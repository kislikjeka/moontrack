package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Compile-time check that ZerionAssetRepository implements sync.ZerionAssetRepository.
var _ sync.ZerionAssetRepository = (*ZerionAssetRepository)(nil)

// ZerionAssetRepository implements the zerion asset repository using PostgreSQL.
type ZerionAssetRepository struct {
	pool *pgxpool.Pool
}

// NewZerionAssetRepository creates a new PostgreSQL zerion asset repository.
func NewZerionAssetRepository(pool *pgxpool.Pool) *ZerionAssetRepository {
	return &ZerionAssetRepository{pool: pool}
}

// Upsert inserts or updates a zerion asset on conflict (symbol, chain_id).
func (r *ZerionAssetRepository) Upsert(ctx context.Context, asset *sync.ZerionAsset) error {
	query := `
		INSERT INTO zerion_assets (symbol, name, chain_id, contract_address, decimals, icon_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (symbol, chain_id) DO UPDATE SET
			name = COALESCE(NULLIF(EXCLUDED.name, ''), zerion_assets.name),
			contract_address = COALESCE(NULLIF(EXCLUDED.contract_address, ''), zerion_assets.contract_address),
			decimals = EXCLUDED.decimals,
			icon_url = COALESCE(NULLIF(EXCLUDED.icon_url, ''), zerion_assets.icon_url),
			updated_at = now()
	`

	_, err := r.pool.Exec(ctx, query,
		asset.Symbol, asset.Name, asset.ChainID,
		asset.ContractAddress, asset.Decimals, asset.IconURL,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert zerion asset: %w", err)
	}

	return nil
}

// GetBySymbol returns a zerion asset by symbol and optional chain_id.
// If chainID is empty, returns the first match for any chain.
func (r *ZerionAssetRepository) GetBySymbol(ctx context.Context, symbol, chainID string) (*sync.ZerionAsset, error) {
	var query string
	var args []any

	if chainID != "" {
		query = `
			SELECT id, symbol, name, chain_id, contract_address, decimals, icon_url, created_at, updated_at
			FROM zerion_assets
			WHERE UPPER(symbol) = UPPER($1) AND chain_id = $2
			LIMIT 1
		`
		args = []any{symbol, chainID}
	} else {
		query = `
			SELECT id, symbol, name, chain_id, contract_address, decimals, icon_url, created_at, updated_at
			FROM zerion_assets
			WHERE UPPER(symbol) = UPPER($1)
			ORDER BY updated_at DESC
			LIMIT 1
		`
		args = []any{symbol}
	}

	var a sync.ZerionAsset
	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&a.ID, &a.Symbol, &a.Name, &a.ChainID,
		&a.ContractAddress, &a.Decimals, &a.IconURL,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get zerion asset: %w", err)
	}

	return &a, nil
}

// GetAllBySymbol returns all zerion assets matching a symbol across all chains.
func (r *ZerionAssetRepository) GetAllBySymbol(ctx context.Context, symbol string) ([]sync.ZerionAsset, error) {
	query := `
		SELECT id, symbol, name, chain_id, contract_address, decimals, icon_url, created_at, updated_at
		FROM zerion_assets
		WHERE UPPER(symbol) = UPPER($1)
		ORDER BY chain_id
	`

	rows, err := r.pool.Query(ctx, query, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to query zerion assets: %w", err)
	}
	defer rows.Close()

	var results []sync.ZerionAsset
	for rows.Next() {
		var a sync.ZerionAsset
		err := rows.Scan(
			&a.ID, &a.Symbol, &a.Name, &a.ChainID,
			&a.ContractAddress, &a.Decimals, &a.IconURL,
			&a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan zerion asset: %w", err)
		}
		results = append(results, a)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating zerion assets: %w", err)
	}

	return results, nil
}
