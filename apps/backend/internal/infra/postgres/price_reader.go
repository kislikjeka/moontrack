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
	"github.com/kislikjeka/moontrack/internal/platform/asset"
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

// Historical returns the best price for ts, choosing the highest-priority
// source among the rows that actually cover ts.
//
// A row covers ts only when ts falls inside the tolerance window implied by
// that row's own granularity (see asset.PricePointCovers) — otherwise the
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

	rows, err := r.pool.Query(ctx, query, assetID, ts, ts.Add(-asset.GranularityDaily.ToleranceWindow()))
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
		if !asset.PricePointCovers(rowTime, ts) {
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
