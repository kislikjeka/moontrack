package postgres

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kislikjeka/moontrack/internal/platform/price"
)

// allowedSources is the whitelist of valid source values for SQL injection safety.
var allowedSources = map[price.Source]bool{
	price.SourceCoinGecko:     true,
	price.SourceZerion:        true,
	price.SourceGeckoTerminal: true,
	price.SourceDefiLlama:     true,
	price.SourceManual:        true,
}

// PriceReader reads price_history with priority-ordered source selection.
// When multiple sources have data for the same asset, the source with the
// lowest priority index wins (regardless of recency).
type PriceReader struct {
	pool     *pgxpool.Pool
	priority []price.Source
}

// NewPriceReader creates a PriceReader with the given source priority list.
// Priority is most-preferred-first, e.g. [coingecko, geckoterminal, defillama].
// Unknown (non-whitelisted) sources are silently ignored.
func NewPriceReader(pool *pgxpool.Pool, priority []price.Source) *PriceReader {
	// Filter to whitelisted sources only.
	filtered := make([]price.Source, 0, len(priority))
	for _, s := range priority {
		if allowedSources[s] {
			filtered = append(filtered, s)
		}
	}
	return &PriceReader{pool: pool, priority: filtered}
}

// buildPriorityCase produces a CASE expression that maps each source to its
// priority rank. Sources not in the list get rank 99.
// Values come from the typed enum — no user input, so interpolation is safe.
func (r *PriceReader) buildPriorityCase() string {
	if len(r.priority) == 0 {
		return "99"
	}
	var sb strings.Builder
	sb.WriteString("CASE source")
	for i, s := range r.priority {
		// s is a whitelisted enum value — safe to interpolate.
		fmt.Fprintf(&sb, " WHEN '%s' THEN %d", string(s), i+1)
	}
	sb.WriteString(" ELSE 99 END")
	return sb.String()
}

// Current returns the latest price for the given asset, choosing the highest-priority
// source available. If no rows exist, returns price.ErrNotFound.
func (r *PriceReader) Current(ctx context.Context, assetID uuid.UUID) (*big.Int, price.Source, error) {
	priorityCase := r.buildPriorityCase()

	query := fmt.Sprintf(`
		WITH latest AS (
			SELECT DISTINCT ON (source) source, time, price_usd
			FROM price_history
			WHERE asset_id = $1
			ORDER BY source, time DESC
		)
		SELECT source, price_usd
		FROM latest
		ORDER BY %s
		LIMIT 1
	`, priorityCase)

	var sourceStr, priceStr string
	err := r.pool.QueryRow(ctx, query, assetID).Scan(&sourceStr, &priceStr)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, "", price.ErrNotFound
		}
		return nil, "", fmt.Errorf("price_reader: current: %w", err)
	}

	v, ok := new(big.Int).SetString(priceStr, 10)
	if !ok {
		return nil, "", fmt.Errorf("price_reader: current: invalid numeric value %q", priceStr)
	}
	return v, price.Source(sourceStr), nil
}

// CurrentBatch returns the latest price for each of the given assets, applying
// the same source priority as Current.
//
// One query, not one per asset. The pre-#59 batch endpoint issued a single
// GetBatchPrices read, and looping Current would turn a 100-asset request into
// 100 round trips. Assets with no price are simply absent from the map, which
// is what lets the caller tell "not priced yet" from a price of zero.
func (r *PriceReader) CurrentBatch(ctx context.Context, assetIDs []uuid.UUID) (map[uuid.UUID]price.Quote, error) {
	out := make(map[uuid.UUID]price.Quote, len(assetIDs))
	if len(assetIDs) == 0 {
		return out, nil
	}
	priorityCase := r.buildPriorityCase()

	// DISTINCT ON (asset_id) with the priority as the leading ORDER BY term
	// picks each asset's best row in one pass, mirroring Current's two-step
	// (latest per source, then best source) without a per-asset subquery.
	query := fmt.Sprintf(`
		SELECT DISTINCT ON (asset_id) asset_id, source, price_usd
		FROM (
			SELECT DISTINCT ON (asset_id, source) asset_id, source, time, price_usd
			FROM price_history
			WHERE asset_id = ANY($1)
			ORDER BY asset_id, source, time DESC
		) latest
		ORDER BY asset_id, %s
	`, priorityCase)

	rows, err := r.pool.Query(ctx, query, assetIDs)
	if err != nil {
		return nil, fmt.Errorf("price_reader: current_batch: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var sourceStr, priceStr string
		if err := rows.Scan(&id, &sourceStr, &priceStr); err != nil {
			return nil, fmt.Errorf("price_reader: current_batch: %w", err)
		}
		v, ok := new(big.Int).SetString(priceStr, 10)
		if !ok {
			return nil, fmt.Errorf("price_reader: current_batch: invalid numeric value %q", priceStr)
		}
		out[id] = price.Quote{PriceUSD: v, Source: price.Source(sourceStr)}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("price_reader: current_batch: %w", err)
	}
	return out, nil
}

// History returns bucketed price points for an asset over [from, to].
//
// It replaces PriceRepository.GetPriceHistory, whose three interval branches
// each spoke asset.PricePoint and whose daily branch read the price_history_daily
// continuous aggregate (#59). This reads the raw hypertable for every interval
// instead: the aggregate is refreshed on a policy that ends an hour in the past
// and it carries no source column, so a backfilled point could be absent from
// it while present in price_history, and the daily series would disagree with
// the current price served beside it.
//
// bucketExpr is a fixed string chosen from a closed set by the caller, never
// user input, so interpolating it cannot inject. last(price_usd, time) picks
// the closing observation of each bucket, matching what the old hourly and
// weekly branches did.
func (r *PriceReader) History(ctx context.Context, assetID uuid.UUID, from, to time.Time, bucket string) ([]price.HistoryPoint, error) {
	var bucketExpr string
	switch bucket {
	case "1h":
		bucketExpr = "time_bucket('1 hour', time)"
	case "1d":
		bucketExpr = "time_bucket('1 day', time)"
	case "1w":
		bucketExpr = "time_bucket('1 week', time)"
	default:
		return nil, fmt.Errorf("price_reader: history: unsupported interval %q", bucket)
	}

	query := fmt.Sprintf(`
		SELECT %s AS bucket,
		       last(price_usd, time) AS price_usd,
		       last(source, time) AS source
		FROM price_history
		WHERE asset_id = $1 AND time >= $2 AND time <= $3
		GROUP BY bucket
		ORDER BY bucket ASC
	`, bucketExpr)

	rows, err := r.pool.Query(ctx, query, assetID, from, to)
	if err != nil {
		return nil, fmt.Errorf("price_reader: history: %w", err)
	}
	defer rows.Close()

	out := make([]price.HistoryPoint, 0)
	for rows.Next() {
		var t time.Time
		var priceStr, sourceStr string
		if err := rows.Scan(&t, &priceStr, &sourceStr); err != nil {
			return nil, fmt.Errorf("price_reader: history: %w", err)
		}
		v, ok := new(big.Int).SetString(priceStr, 10)
		if !ok {
			return nil, fmt.Errorf("price_reader: history: invalid numeric value %q", priceStr)
		}
		out = append(out, price.HistoryPoint{Time: t, PriceUSD: v, Source: price.Source(sourceStr)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("price_reader: history: %w", err)
	}
	return out, nil
}

// Historical returns the best price for ts, choosing the highest-priority
// source among the rows that actually cover ts.
//
// A row covers ts only when ts falls inside the tolerance window implied by
// that row's own granularity (see price.PricePointCovers) — otherwise the
// nearest-at-or-before row would let an arbitrarily stale spot point stand in
// for a point-in-time price. Returns price.ErrNotFound when no row covers ts.
func (r *PriceReader) Historical(ctx context.Context, assetID uuid.UUID, ts time.Time) (*price.HistoricalPrice, price.Source, error) {
	priorityCase := r.buildPriorityCase()

	// The widest window any granularity implies is a day, so a row older than
	// that can never cover ts. That bound keeps the candidate set small; the
	// authoritative per-row check happens in Go, where the window is derived
	// from each row's own granularity.
	//
	// Every candidate is considered, not just the nearest one per source: the
	// nearest row may well be a spot point that fails its (zero-width) window,
	// while an older aligned point from the same source still covers ts.
	// Ordering is priority first, then recency, so the first covering row is
	// the best answer.
	query := fmt.Sprintf(`
		SELECT source, time, price_usd
		FROM price_history
		WHERE asset_id = $1 AND time <= $2 AND time >= $3
		ORDER BY %s, time DESC
	`, priorityCase)

	rows, err := r.pool.Query(ctx, query, assetID, ts, ts.Add(-price.GranularityDaily.ToleranceWindow()))
	if err != nil {
		return nil, "", fmt.Errorf("price_reader: historical: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sourceStr, priceStr string
		var rowTime time.Time
		if err := rows.Scan(&sourceStr, &rowTime, &priceStr); err != nil {
			return nil, "", fmt.Errorf("price_reader: historical: %w", err)
		}
		// Rows arrive in priority order; take the first one that covers ts.
		if !price.PricePointCovers(rowTime, ts) {
			continue
		}
		v, ok := new(big.Int).SetString(priceStr, 10)
		if !ok {
			return nil, "", fmt.Errorf("price_reader: historical: invalid numeric value %q", priceStr)
		}
		return &price.HistoricalPrice{
			PriceUSD:   v,
			Timestamp:  rowTime,
			Confidence: 1.0,
		}, price.Source(sourceStr), nil
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("price_reader: historical: %w", err)
	}

	return nil, "", price.ErrNotFound
}
