package postgres

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

// PriceRepository handles price history persistence operations
type PriceRepository struct {
	pool *pgxpool.Pool
}

// NewPriceRepository creates a new PostgreSQL price repository
func NewPriceRepository(pool *pgxpool.Pool) *PriceRepository {
	return &PriceRepository{pool: pool}
}

// RecordPrice inserts or updates a price record
func (r *PriceRepository) RecordPrice(ctx context.Context, price *asset.PricePoint) error {
	if err := price.Validate(); err != nil {
		return fmt.Errorf("invalid price: %w", err)
	}

	query := `
		INSERT INTO price_history (time, asset_id, price_usd, volume_24h, market_cap, source)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (asset_id, time) DO UPDATE SET
			price_usd = EXCLUDED.price_usd,
			volume_24h = EXCLUDED.volume_24h,
			market_cap = EXCLUDED.market_cap,
			source = EXCLUDED.source
	`

	var volume, marketCap *string
	if price.Volume24h != nil {
		v := price.Volume24h.String()
		volume = &v
	}
	if price.MarketCap != nil {
		m := price.MarketCap.String()
		marketCap = &m
	}

	_, err := r.pool.Exec(ctx, query,
		price.Time,
		price.AssetID,
		price.PriceUSD.String(),
		volume,
		marketCap,
		price.Source,
	)
	if err != nil {
		return fmt.Errorf("failed to record price: %w", err)
	}

	return nil
}

// GetCurrentPrice retrieves the most recent price for an asset
func (r *PriceRepository) GetCurrentPrice(ctx context.Context, assetID uuid.UUID) (*asset.PricePoint, error) {
	query := `
		SELECT time, asset_id, price_usd, volume_24h, market_cap, source
		FROM price_history
		WHERE asset_id = $1
		ORDER BY time DESC
		LIMIT 1
	`

	price, err := r.scanPricePoint(r.pool.QueryRow(ctx, query, assetID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, asset.ErrNoPriceData
		}
		return nil, fmt.Errorf("failed to get current price: %w", err)
	}

	return price, nil
}

// GetPriceAt retrieves the price at or before a specific time
func (r *PriceRepository) GetPriceAt(ctx context.Context, assetID uuid.UUID, at time.Time) (*asset.PricePoint, error) {
	query := `
		SELECT time, asset_id, price_usd, volume_24h, market_cap, source
		FROM price_history
		WHERE asset_id = $1 AND time <= $2
		ORDER BY time DESC
		LIMIT 1
	`

	price, err := r.scanPricePoint(r.pool.QueryRow(ctx, query, assetID, at))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, asset.ErrNoPriceData
		}
		return nil, fmt.Errorf("failed to get price at time: %w", err)
	}

	return price, nil
}

// GetPriceHistory retrieves price history for a time range with specified interval
func (r *PriceRepository) GetPriceHistory(ctx context.Context, query *asset.PriceHistoryQuery) ([]asset.PricePoint, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}

	switch query.Interval {
	case asset.PriceIntervalHourly:
		return r.getHourlyPrices(ctx, query)
	case asset.PriceIntervalDaily:
		return r.getDailyPrices(ctx, query)
	case asset.PriceIntervalWeekly:
		return r.getWeeklyPrices(ctx, query)
	default:
		return nil, asset.ErrInvalidInterval
	}
}

// getHourlyPrices retrieves hourly prices from raw price_history table
func (r *PriceRepository) getHourlyPrices(ctx context.Context, query *asset.PriceHistoryQuery) ([]asset.PricePoint, error) {
	sqlQuery := `
		SELECT
			time_bucket('1 hour', time) AS bucket,
			asset_id,
			last(price_usd, time) AS price_usd,
			avg(volume_24h) AS volume_24h,
			last(market_cap, time) AS market_cap,
			last(source, time) AS source
		FROM price_history
		WHERE asset_id = $1 AND time >= $2 AND time <= $3
		GROUP BY bucket, asset_id
		ORDER BY bucket ASC
	`

	rows, err := r.pool.Query(ctx, sqlQuery, query.AssetID, query.From, query.To)
	if err != nil {
		return nil, fmt.Errorf("failed to query hourly prices: %w", err)
	}
	defer rows.Close()

	return r.scanPricePoints(rows)
}

// getDailyPrices retrieves daily OHLCV from continuous aggregate
func (r *PriceRepository) getDailyPrices(ctx context.Context, query *asset.PriceHistoryQuery) ([]asset.PricePoint, error) {
	// Use continuous aggregate for daily data
	sqlQuery := `
		SELECT day, asset_id, close, avg_volume
		FROM price_history_daily
		WHERE asset_id = $1 AND day >= $2 AND day <= $3
		ORDER BY day ASC
	`

	rows, err := r.pool.Query(ctx, sqlQuery, query.AssetID, query.From, query.To)
	if err != nil {
		return nil, fmt.Errorf("failed to query daily prices: %w", err)
	}
	defer rows.Close()

	return r.scanDailyPrices(rows)
}

// getWeeklyPrices retrieves weekly aggregated prices
func (r *PriceRepository) getWeeklyPrices(ctx context.Context, query *asset.PriceHistoryQuery) ([]asset.PricePoint, error) {
	sqlQuery := `
		SELECT
			time_bucket('1 week', time) AS bucket,
			asset_id,
			last(price_usd, time) AS price_usd,
			avg(volume_24h) AS volume_24h,
			last(market_cap, time) AS market_cap,
			last(source, time) AS source
		FROM price_history
		WHERE asset_id = $1 AND time >= $2 AND time <= $3
		GROUP BY bucket, asset_id
		ORDER BY bucket ASC
	`

	rows, err := r.pool.Query(ctx, sqlQuery, query.AssetID, query.From, query.To)
	if err != nil {
		return nil, fmt.Errorf("failed to query weekly prices: %w", err)
	}
	defer rows.Close()

	return r.scanPricePoints(rows)
}

// GetOHLCV retrieves OHLCV data for a time range
func (r *PriceRepository) GetOHLCV(ctx context.Context, query *asset.PriceHistoryQuery) ([]asset.OHLCV, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}

	sqlQuery := `
		SELECT day, asset_id, open, high, low, close, avg_volume
		FROM price_history_daily
		WHERE asset_id = $1 AND day >= $2 AND day <= $3
		ORDER BY day ASC
	`

	rows, err := r.pool.Query(ctx, sqlQuery, query.AssetID, query.From, query.To)
	if err != nil {
		return nil, fmt.Errorf("failed to query OHLCV: %w", err)
	}
	defer rows.Close()

	var ohlcvs []asset.OHLCV
	for rows.Next() {
		var ohlcv asset.OHLCV
		var openStr, highStr, lowStr, closeStr string
		var avgVolStr *string

		err := rows.Scan(&ohlcv.Time, &ohlcv.AssetID, &openStr, &highStr, &lowStr, &closeStr, &avgVolStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan OHLCV: %w", err)
		}

		ohlcv.Open, _ = new(big.Int).SetString(openStr, 10)
		ohlcv.High, _ = new(big.Int).SetString(highStr, 10)
		ohlcv.Low, _ = new(big.Int).SetString(lowStr, 10)
		ohlcv.Close, _ = new(big.Int).SetString(closeStr, 10)
		if avgVolStr != nil {
			ohlcv.AvgVolume, _ = new(big.Int).SetString(*avgVolStr, 10)
		}

		ohlcvs = append(ohlcvs, ohlcv)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating OHLCV: %w", err)
	}

	return ohlcvs, nil
}

// GetRecentPrice retrieves price within the last N minutes (for cache fallback)
func (r *PriceRepository) GetRecentPrice(ctx context.Context, assetID uuid.UUID, maxAge time.Duration) (*asset.PricePoint, error) {
	cutoff := time.Now().Add(-maxAge)

	query := `
		SELECT time, asset_id, price_usd, volume_24h, market_cap, source
		FROM price_history
		WHERE asset_id = $1 AND time >= $2
		ORDER BY time DESC
		LIMIT 1
	`

	price, err := r.scanPricePoint(r.pool.QueryRow(ctx, query, assetID, cutoff))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, asset.ErrNoPriceData
		}
		return nil, fmt.Errorf("failed to get recent price: %w", err)
	}

	return price, nil
}

// scanPricePoint scans a single row into a PricePoint
func (r *PriceRepository) scanPricePoint(row pgx.Row) (*asset.PricePoint, error) {
	var price asset.PricePoint
	var priceStr string
	var volumeStr, marketCapStr *string
	var source string

	err := row.Scan(&price.Time, &price.AssetID, &priceStr, &volumeStr, &marketCapStr, &source)
	if err != nil {
		return nil, err
	}

	price.PriceUSD, _ = new(big.Int).SetString(priceStr, 10)
	if volumeStr != nil {
		price.Volume24h, _ = new(big.Int).SetString(*volumeStr, 10)
	}
	if marketCapStr != nil {
		price.MarketCap, _ = new(big.Int).SetString(*marketCapStr, 10)
	}
	price.Source = asset.PriceSource(source)

	return &price, nil
}

// scanPricePoints scans multiple rows into a slice of PricePoints
func (r *PriceRepository) scanPricePoints(rows pgx.Rows) ([]asset.PricePoint, error) {
	var prices []asset.PricePoint

	for rows.Next() {
		var price asset.PricePoint
		var priceStr string
		var volumeStr, marketCapStr *string
		var source string

		err := rows.Scan(&price.Time, &price.AssetID, &priceStr, &volumeStr, &marketCapStr, &source)
		if err != nil {
			return nil, fmt.Errorf("failed to scan price: %w", err)
		}

		price.PriceUSD, _ = new(big.Int).SetString(priceStr, 10)
		if volumeStr != nil {
			price.Volume24h, _ = new(big.Int).SetString(*volumeStr, 10)
		}
		if marketCapStr != nil {
			price.MarketCap, _ = new(big.Int).SetString(*marketCapStr, 10)
		}
		price.Source = asset.PriceSource(source)

		prices = append(prices, price)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating prices: %w", err)
	}

	return prices, nil
}

// scanDailyPrices scans daily aggregate rows into PricePoints
func (r *PriceRepository) scanDailyPrices(rows pgx.Rows) ([]asset.PricePoint, error) {
	var prices []asset.PricePoint

	for rows.Next() {
		var price asset.PricePoint
		var closeStr string
		var avgVolStr *string

		err := rows.Scan(&price.Time, &price.AssetID, &closeStr, &avgVolStr)
		if err != nil {
			return nil, fmt.Errorf("failed to scan daily price: %w", err)
		}

		price.PriceUSD, _ = new(big.Int).SetString(closeStr, 10)
		if avgVolStr != nil {
			price.Volume24h, _ = new(big.Int).SetString(*avgVolStr, 10)
		}
		price.Source = asset.PriceSourceCoinGecko

		prices = append(prices, price)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating daily prices: %w", err)
	}

	return prices, nil
}
