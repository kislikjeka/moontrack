// Package main is a one-shot CLI that enqueues backfill jobs for legacy tax lots
// whose auto_cost_basis_per_unit is NULL or zero (from the pre-Task-15 era where
// prices were not resolved via the fallback provider pipeline).
//
// Usage:
//
//	go run ./cmd/backfill-legacy-prices            # dry-run (default, safe)
//	go run ./cmd/backfill-legacy-prices -dry-run=false  # actually flip lots
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const batchSize = 1000

// lotRow holds the data we need for one legacy lot.
type lotRow struct {
	id         uuid.UUID
	assetID    uuid.UUID
	acquiredAt time.Time
}

func main() {
	dryRun := flag.Bool("dry-run", true, "Print affected rows without modifying data (default: true)")
	force := flag.Bool("force", false, "Allow destructive mode without FEATURE_PRICE_FALLBACK=true (dangerous)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Safety guard: in destructive mode, require the price-fallback worker to be
	// enabled. Otherwise, flipping lots to "pending" strands them forever because
	// nothing will re-resolve the prices.
	//
	// The --force flag lets an operator bypass the check when they know the
	// worker is running elsewhere (e.g. the user has verified a separate
	// deployment). We still print a clear warning so the override is logged.
	if !*dryRun {
		flag := os.Getenv("FEATURE_PRICE_FALLBACK")
		if flag != "true" && !*force {
			fmt.Fprintln(os.Stderr,
				"ERROR: destructive mode requires FEATURE_PRICE_FALLBACK=true so the "+
					"backfill worker will pick up flipped lots. Without it, lots flipped to "+
					"'pending' will never be re-resolved and will remain stuck.")
			fmt.Fprintln(os.Stderr,
				"       Options: export FEATURE_PRICE_FALLBACK=true (recommended), OR "+
					"re-run with --force if you have verified the worker is running elsewhere.")
			os.Exit(2)
		}
		if *force && flag != "true" {
			fmt.Fprintln(os.Stderr,
				"WARN: --force supplied with FEATURE_PRICE_FALLBACK!=\"true\". Proceeding, "+
					"but flipped lots will be stuck until a backfill worker starts.")
		}
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "ERROR: DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to ping database: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Connected to database.")

	// Query legacy lots: price_status='resolved' but auto_cost_basis_per_unit is NULL or 0,
	// and the asset has on-chain identity (chain_id + contract_address) so the fallback
	// provider pipeline can price it.
	const query = `
		SELECT tl.id, tl.asset_id, tl.acquired_at
		FROM tax_lots tl
		JOIN assets a ON a.id = tl.asset_id
		WHERE (tl.auto_cost_basis_per_unit IS NULL OR tl.auto_cost_basis_per_unit = 0)
		  AND tl.price_status = 'resolved'
		  AND a.chain_id IS NOT NULL
		  AND a.contract_address IS NOT NULL
		ORDER BY tl.acquired_at
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to query legacy lots: %v\n", err)
		os.Exit(1)
	}

	var lots []lotRow
	for rows.Next() {
		var r lotRow
		if err := rows.Scan(&r.id, &r.assetID, &r.acquiredAt); err != nil {
			rows.Close()
			fmt.Fprintf(os.Stderr, "ERROR: failed to scan row: %v\n", err)
			os.Exit(1)
		}
		lots = append(lots, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: row iteration error: %v\n", err)
		os.Exit(1)
	}

	total := len(lots)
	fmt.Printf("Found %d legacy lot(s) with zero/NULL cost basis and on-chain asset identity.\n", total)

	if total == 0 {
		fmt.Println("Nothing to do.")
		return
	}

	if *dryRun {
		fmt.Println("\n[DRY-RUN] First 10 lots:")
		limit := total
		if limit > 10 {
			limit = 10
		}
		for i := 0; i < limit; i++ {
			r := lots[i]
			fmt.Printf("  lot_id=%-36s  asset_id=%-36s  acquired_at=%s\n",
				r.id, r.assetID, r.acquiredAt.Format(time.RFC3339))
		}
		if total > 10 {
			fmt.Printf("  ... and %d more.\n", total-10)
		}
		fmt.Println("\n[DRY-RUN] No data was modified. Re-run with -dry-run=false to apply changes.")
		return
	}

	// ── Non-dry-run: safety banner with countdown ────────────────────────────
	fmt.Printf("\n!!! Running in DESTRUCTIVE mode. %d lot(s) will be flipped to pending.\n", total)
	fmt.Println("!!! Press Ctrl-C within 5 seconds to abort.")
	for i := 5; i > 0; i-- {
		select {
		case <-ctx.Done():
			fmt.Println("\nAborted by user.")
			return
		case <-time.After(time.Second):
			fmt.Printf("  %d...\n", i)
		}
	}
	fmt.Println("Proceeding...")

	// Process in batches of batchSize.
	totalLotsFlipped := 0
	totalJobsEnqueued := 0

	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		batch := lots[start:end]

		flipped, enqueued, err := processBatch(ctx, pool, batch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: batch [%d, %d) failed: %v\n", start, end, err)
			os.Exit(1)
		}
		totalLotsFlipped += flipped
		totalJobsEnqueued += enqueued

		fmt.Printf("Batch [%d, %d): flipped=%d  jobs_enqueued=%d\n", start, end, flipped, enqueued)

		// Check for cancellation between batches.
		select {
		case <-ctx.Done():
			fmt.Printf("\nInterrupted after %d lot(s) flipped, %d job(s) enqueued.\n",
				totalLotsFlipped, totalJobsEnqueued)
			return
		default:
		}
	}

	fmt.Printf("\nDone. Total lots flipped to pending: %d. Total backfill jobs enqueued: %d.\n",
		totalLotsFlipped, totalJobsEnqueued)
}

// processBatch runs the UPDATE + INSERT inside a single transaction for one batch.
func processBatch(ctx context.Context, pool *pgxpool.Pool, batch []lotRow) (flipped, enqueued int, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Collect IDs and unique (asset_id, target_time) pairs.
	ids := make([]uuid.UUID, len(batch))
	type jobKey struct {
		assetID    uuid.UUID
		targetTime time.Time
	}
	seen := make(map[jobKey]struct{})
	var jobs []jobKey

	for i, r := range batch {
		ids[i] = r.id
		key := jobKey{
			assetID:    r.assetID,
			targetTime: r.acquiredAt.UTC().Truncate(time.Minute),
		}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			jobs = append(jobs, key)
		}
	}

	// 1. Flip lots back to pending so the worker will re-resolve them.
	ct, err := tx.Exec(ctx, `
		UPDATE tax_lots
		SET price_status = 'pending',
		    price_resolution_attempts = 0,
		    price_next_retry_at = NULL,
		    auto_cost_basis_per_unit = NULL
		WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return 0, 0, fmt.Errorf("update tax_lots: %w", err)
	}
	flipped = int(ct.RowsAffected())

	// 2. Enqueue one price_backfill_job per unique (asset_id, target_time).
	for _, j := range jobs {
		ct2, err := tx.Exec(ctx, `
			INSERT INTO price_backfill_jobs (asset_id, target_time)
			VALUES ($1, $2)
			ON CONFLICT (asset_id, target_time) DO NOTHING
		`, j.assetID, j.targetTime)
		if err != nil {
			return 0, 0, fmt.Errorf("insert price_backfill_job: %w", err)
		}
		enqueued += int(ct2.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit tx: %w", err)
	}
	return flipped, enqueued, nil
}
