package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// AssetRepository handles asset persistence operations
type AssetRepository struct {
	pool *pgxpool.Pool
}

// NewAssetRepository creates a new PostgreSQL asset repository
func NewAssetRepository(pool *pgxpool.Pool) *AssetRepository {
	return &AssetRepository{pool: pool}
}

// GetByID retrieves an asset by its UUID
func (r *AssetRepository) GetByID(ctx context.Context, id uuid.UUID) (*asset.Asset, error) {
	query := `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type, chain_id,
		       contract_address, market_cap_rank, is_active, metadata, created_at, updated_at
		FROM assets
		WHERE id = $1
	`

	a, err := r.scanAsset(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, asset.ErrAssetNotFound
		}
		return nil, fmt.Errorf("failed to get asset: %w", err)
	}

	return a, nil
}

// GetBySymbol retrieves an asset by symbol, optionally filtered by chain
// Returns ErrAmbiguousSymbol if symbol exists on multiple chains and chainID is nil
func (r *AssetRepository) GetBySymbol(ctx context.Context, symbol string, chainID *string) (*asset.Asset, error) {
	// First check how many assets match this symbol
	if chainID == nil {
		assets, err := r.GetAllBySymbol(ctx, symbol)
		if err != nil {
			return nil, err
		}

		if len(assets) == 0 {
			return nil, asset.ErrAssetNotFound
		}

		if len(assets) > 1 {
			chains := make([]string, len(assets))
			for i, a := range assets {
				if a.ChainID != nil {
					chains[i] = *a.ChainID
				} else {
					chains[i] = "native"
				}
			}
			return nil, asset.NewAmbiguousSymbolError(symbol, chains)
		}

		return &assets[0], nil
	}

	// chainID is specified, query directly
	query := `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type, chain_id,
		       contract_address, market_cap_rank, is_active, metadata, created_at, updated_at
		FROM assets
		WHERE UPPER(symbol) = UPPER($1) AND chain_id = $2
	`

	a, err := r.scanAsset(r.pool.QueryRow(ctx, query, symbol, *chainID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, asset.ErrAssetNotFound
		}
		return nil, fmt.Errorf("failed to get asset: %w", err)
	}

	return a, nil
}

// GetByCoinGeckoID retrieves an asset by its CoinGecko ID
func (r *AssetRepository) GetByCoinGeckoID(ctx context.Context, coinGeckoID string) (*asset.Asset, error) {
	query := `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type, chain_id,
		       contract_address, market_cap_rank, is_active, metadata, created_at, updated_at
		FROM assets
		WHERE coingecko_id = $1
	`

	a, err := r.scanAsset(r.pool.QueryRow(ctx, query, coinGeckoID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, asset.ErrAssetNotFound
		}
		return nil, fmt.Errorf("failed to get asset: %w", err)
	}

	return a, nil
}

// GetAllBySymbol retrieves all assets matching a symbol across all chains
func (r *AssetRepository) GetAllBySymbol(ctx context.Context, symbol string) ([]asset.Asset, error) {
	query := `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type, chain_id,
		       contract_address, market_cap_rank, is_active, metadata, created_at, updated_at
		FROM assets
		WHERE UPPER(symbol) = UPPER($1) AND is_active = true
		ORDER BY COALESCE(market_cap_rank, 999999), chain_id NULLS FIRST
	`

	rows, err := r.pool.Query(ctx, query, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to query assets: %w", err)
	}
	defer rows.Close()

	return r.scanAssets(rows)
}

// Create creates a new asset in the database
func (r *AssetRepository) Create(ctx context.Context, a *asset.Asset) error {
	if err := a.Validate(); err != nil {
		return fmt.Errorf("invalid asset: %w", err)
	}

	query := `
		INSERT INTO assets (id, symbol, name, coingecko_id, decimals, asset_type, chain_id,
		                    contract_address, market_cap_rank, is_active, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err := r.pool.Exec(ctx, query,
		a.ID,
		a.Symbol,
		a.Name,
		a.CoinGeckoID,
		a.Decimals,
		a.AssetType,
		a.ChainID,
		a.ContractAddress,
		a.MarketCapRank,
		a.IsActive,
		a.Metadata,
		a.CreatedAt,
		a.UpdatedAt,
	)
	if err != nil {
		if isAssetUniqueViolation(err) {
			return asset.ErrDuplicateAsset
		}
		return fmt.Errorf("failed to create asset: %w", err)
	}

	return nil
}

// Search searches for assets by query string (matches symbol or name)
func (r *AssetRepository) Search(ctx context.Context, query string, limit int) ([]asset.Asset, error) {
	if limit <= 0 {
		limit = 20
	}

	sqlQuery := `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type, chain_id,
		       contract_address, market_cap_rank, is_active, metadata, created_at, updated_at
		FROM assets
		WHERE is_active = true AND (
			UPPER(symbol) LIKE UPPER($1) OR
			UPPER(name) LIKE UPPER($1)
		)
		ORDER BY
			CASE WHEN UPPER(symbol) = UPPER($2) THEN 0 ELSE 1 END,
			COALESCE(market_cap_rank, 999999),
			chain_id NULLS FIRST
		LIMIT $3
	`

	searchPattern := query + "%"
	rows, err := r.pool.Query(ctx, sqlQuery, searchPattern, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search assets: %w", err)
	}
	defer rows.Close()

	return r.scanAssets(rows)
}

// GetActiveAssets retrieves all active assets (for background price updater)
func (r *AssetRepository) GetActiveAssets(ctx context.Context) ([]asset.Asset, error) {
	query := `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type, chain_id,
		       contract_address, market_cap_rank, is_active, metadata, created_at, updated_at
		FROM assets
		WHERE is_active = true
		ORDER BY COALESCE(market_cap_rank, 999999)
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active assets: %w", err)
	}
	defer rows.Close()

	return r.scanAssets(rows)
}

// GetByChain retrieves all assets on a specific chain
func (r *AssetRepository) GetByChain(ctx context.Context, chainID string) ([]asset.Asset, error) {
	query := `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type, chain_id,
		       contract_address, market_cap_rank, is_active, metadata, created_at, updated_at
		FROM assets
		WHERE chain_id = $1 AND is_active = true
		ORDER BY COALESCE(market_cap_rank, 999999)
	`

	rows, err := r.pool.Query(ctx, query, chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to query assets by chain: %w", err)
	}
	defer rows.Close()

	return r.scanAssets(rows)
}

// scanAsset scans a single row into an Asset
func (r *AssetRepository) scanAsset(row pgx.Row) (*asset.Asset, error) {
	var a asset.Asset
	var chainID, contractAddress sql.NullString
	var marketCapRank sql.NullInt32
	var assetType string

	err := row.Scan(
		&a.ID,
		&a.Symbol,
		&a.Name,
		&a.CoinGeckoID,
		&a.Decimals,
		&assetType,
		&chainID,
		&contractAddress,
		&marketCapRank,
		&a.IsActive,
		&a.Metadata,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	a.AssetType = asset.AssetType(assetType)
	if chainID.Valid {
		a.ChainID = &chainID.String
	}
	if contractAddress.Valid {
		a.ContractAddress = &contractAddress.String
	}
	if marketCapRank.Valid {
		rank := int(marketCapRank.Int32)
		a.MarketCapRank = &rank
	}

	// Initialize metadata if null
	if a.Metadata == nil {
		a.Metadata = json.RawMessage("{}")
	}

	return &a, nil
}

// scanAssets scans multiple rows into a slice of Assets
func (r *AssetRepository) scanAssets(rows pgx.Rows) ([]asset.Asset, error) {
	var assets []asset.Asset

	for rows.Next() {
		var a asset.Asset
		var chainID, contractAddress sql.NullString
		var marketCapRank sql.NullInt32
		var assetType string

		err := rows.Scan(
			&a.ID,
			&a.Symbol,
			&a.Name,
			&a.CoinGeckoID,
			&a.Decimals,
			&assetType,
			&chainID,
			&contractAddress,
			&marketCapRank,
			&a.IsActive,
			&a.Metadata,
			&a.CreatedAt,
			&a.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan asset: %w", err)
		}

		a.AssetType = asset.AssetType(assetType)
		if chainID.Valid {
			a.ChainID = &chainID.String
		}
		if contractAddress.Valid {
			a.ContractAddress = &contractAddress.String
		}
		if marketCapRank.Valid {
			rank := int(marketCapRank.Int32)
			a.MarketCapRank = &rank
		}
		if a.Metadata == nil {
			a.Metadata = json.RawMessage("{}")
		}

		assets = append(assets, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating assets: %w", err)
	}

	return assets, nil
}

// isAssetUniqueViolation checks if the error is a unique constraint violation
func isAssetUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "duplicate key") ||
		strings.Contains(errStr, "unique constraint") ||
		strings.Contains(errStr, "23505")
}
