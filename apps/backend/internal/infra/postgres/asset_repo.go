package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// evmChains is the set of chains whose contract addresses follow the EVM
// 20-byte hex format (0x + 40 lowercase hex chars). We validate strictly to
// prevent duplicate rows from homoglyph / trailing-whitespace attacks.
var evmChains = map[string]struct{}{
	"ethereum":  {},
	"arbitrum":  {},
	"optimism":  {},
	"base":      {},
	"polygon":   {},
	"bnb-chain": {},
	"avalanche": {},
	"linea":     {},
	"zksync":    {},
	"scroll":    {},
}

// evmAddressRe matches a canonical lowercase EVM address.
var evmAddressRe = regexp.MustCompile(`^0x[a-f0-9]{40}$`)

// symbolCapBytes caps user/provider-supplied asset symbols.
// Enforced at the trust boundary as defense-in-depth alongside the DB column width.
const symbolCapBytes = 32

// nameCapBytes caps user/provider-supplied asset names.
const nameCapBytes = 128

// normalizeContractAddress trims, lowercases, and (for EVM chains) shape-validates
// a contract address. For Solana and unknown chains it only trims + lowercases.
//
// Returns asset.ErrInvalidContractAddress if the EVM shape check fails.
//
// Note: a single source of truth for contract-address normalization. All callers
// that write to assets.contract_address should go through this function (or
// through the repository methods that call it).
func normalizeContractAddress(chainID, addr string) (string, error) {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" {
		return "", nil
	}
	if _, ok := evmChains[chainID]; ok {
		if !evmAddressRe.MatchString(addr) {
			return "", fmt.Errorf("%w: %q on chain %q", asset.ErrInvalidContractAddress, addr, chainID)
		}
	}
	// Solana / unknown chains: trim+lower only. (Solana addresses are base58 32..44
	// chars; we don't enforce strictly — we only normalize.)
	return addr, nil
}

// sanitizeProviderField strips control / line-separating runes and caps the
// length. Symbols and names from 3rd-party providers are attacker-influenced,
// so in addition to the historical ASCII-control handling we also strip
// UTF-8 line separators (U+2028, U+2029), NEL (U+0085), DEL (U+007F) and
// C1 controls (0x80..0x9F) — all of which can forge log lines when the
// value is emitted into a structured log.
//
// We walk the input rune-by-rune (via utf8.DecodeRuneInString) rather than
// byte-by-byte so multi-byte line separators are actually recognized. The
// cap is applied byte-wise but snapped to a rune boundary so a truncated
// tail never leaves a dangling partial sequence.
func sanitizeProviderField(s string, maxLen int) string {
	if len(s) > maxLen {
		cut := maxLen
		for cut > 0 && cut < len(s) && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			b = append(b, ' ')
		case r < 0x20, // C0 controls incl. \t, \n, \r
			r == 0x7F,                   // DEL
			r == 0x85,                   // NEL
			r >= 0x80 && r < 0xA0,       // C1 controls
			r == 0x2028, r == 0x2029:    // LINE/PARAGRAPH SEPARATOR
			b = append(b, ' ')
		default:
			b = utf8.AppendRune(b, r)
		}
		i += size
	}
	return string(b)
}

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

// UpsertByOnChainIdentity uses the partial unique index idx_assets_onchain_identity
// to dedupe on (chain_id, contract_address).
//
// This is the trust boundary for provider-supplied identity. We normalize the
// contract address (trim+lower, shape-check for EVM chains) and cap symbol/name
// length + strip control bytes here so all callers get the same treatment.
func (r *AssetRepository) UpsertByOnChainIdentity(
	ctx context.Context,
	chainID, contractAddress string,
	symbol, name string,
	decimals int,
) (*asset.Asset, bool, error) {
	// Normalize & validate the address (single source of truth).
	addrLower, err := normalizeContractAddress(chainID, contractAddress)
	if err != nil {
		return nil, false, err
	}

	// Cap + sanitize provider-supplied symbol/name. Defense in depth at the DB
	// trust boundary, regardless of upstream limits.
	symbol = sanitizeProviderField(symbol, symbolCapBytes)
	name = sanitizeProviderField(name, nameCapBytes)

	// Try to find an existing row first.
	row := r.pool.QueryRow(ctx, `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type,
		       chain_id, contract_address, market_cap_rank, is_active,
		       metadata, created_at, updated_at
		FROM assets
		WHERE chain_id = $1 AND contract_address = $2
	`, chainID, addrLower)

	existing, err := r.scanAssetNullableCG(row)
	if err == nil {
		return existing, false, nil
	}
	if err != pgx.ErrNoRows {
		return nil, false, fmt.Errorf("failed to lookup asset by on-chain identity: %w", err)
	}

	// Not found — create. Race-safe: partial unique index catches concurrent inserts.
	newAsset := &asset.Asset{
		ID:              uuid.New(),
		Symbol:          symbol,
		Name:            name,
		Decimals:        decimals,
		AssetType:       asset.AssetTypeCrypto,
		ChainID:         &chainID,
		ContractAddress: &addrLower,
		IsActive:        true,
		Metadata:        json.RawMessage("{}"),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO assets (id, symbol, name, coingecko_id, decimals, asset_type,
		                    chain_id, contract_address, is_active, metadata,
		                    created_at, updated_at)
		VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, TRUE, $8, $9, $10)
		ON CONFLICT (chain_id, contract_address)
		  WHERE chain_id IS NOT NULL AND contract_address IS NOT NULL
		DO NOTHING
	`, newAsset.ID, symbol, name, decimals, string(newAsset.AssetType),
		chainID, addrLower, newAsset.Metadata, newAsset.CreatedAt, newAsset.UpdatedAt)
	if err != nil {
		return nil, false, fmt.Errorf("failed to insert asset: %w", err)
	}

	// Re-select in case the insert hit the conflict and did nothing (race).
	row = r.pool.QueryRow(ctx, `
		SELECT id, symbol, name, coingecko_id, decimals, asset_type,
		       chain_id, contract_address, market_cap_rank, is_active,
		       metadata, created_at, updated_at
		FROM assets
		WHERE chain_id = $1 AND contract_address = $2
	`, chainID, addrLower)

	out, err := r.scanAssetNullableCG(row)
	if err != nil {
		return nil, false, fmt.Errorf("failed to re-read upserted asset: %w", err)
	}
	created := out.ID == newAsset.ID
	return out, created, nil
}

// scanAssetNullableCG scans a row where coingecko_id may be NULL.
func (r *AssetRepository) scanAssetNullableCG(row pgx.Row) (*asset.Asset, error) {
	var a asset.Asset
	var cgID sql.NullString
	var chainID, contractAddress sql.NullString
	var marketCapRank sql.NullInt32
	var assetType string

	err := row.Scan(
		&a.ID, &a.Symbol, &a.Name, &cgID, &a.Decimals,
		&assetType, &chainID, &contractAddress, &marketCapRank, &a.IsActive,
		&a.Metadata, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	a.AssetType = asset.AssetType(assetType)
	if cgID.Valid {
		a.CoinGeckoID = cgID.String
	}
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

	return &a, nil
}
