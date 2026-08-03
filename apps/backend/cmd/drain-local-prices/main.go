// Command drain-local-prices resolves pending tax lots and disposals using
// ONLY the price points already stored in price_history. It never contacts a
// price provider.
//
// It exists to answer the question #74 turns on: once "price unknown" is
// representable, does a price that is already known actually reach the lot?
// The production path to that answer is the backfill worker, which calls a
// provider first; this tool substitutes the local reader for the provider and
// keeps everything downstream — the same tolerance window, the same
// PriceResolvedHook, the same repository — so what it exercises is the real
// resolution path rather than a re-implementation of it.
//
// Lots whose moment no stored point covers are left pending. That is the
// correct outcome, not a failure: the honest state for an unknown price is
// pending, and the backfill worker will fetch it from the provider later.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "postgres DSN")
	dry := flag.Bool("dry-run", false, "report what would resolve without writing")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("missing -dsn (or DATABASE_URL)")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	reader := postgres.NewPriceReader(pool, []price.Source{
		price.SourceManual,
		price.SourceCoinGecko,
		price.SourceDefiLlama,
		price.SourceGeckoTerminal,
		price.SourceZerion,
	})
	lotRepo := postgres.NewTaxLotRepository(pool)
	hook := ledger.NewPriceResolvedHook(lotRepo, logger.New("production", os.Stderr))

	// Every (asset, minute) a pending lot or disposal is waiting on.
	type target struct {
		asset uuid.UUID
		at    time.Time
	}
	seen := map[target]bool{}
	var targets []target

	rows, err := pool.Query(ctx, `
		SELECT asset, date_trunc('minute', acquired_at) FROM tax_lots WHERE price_status = 'pending'
		UNION
		SELECT tl.asset, date_trunc('minute', ld.disposed_at)
		FROM lot_disposals ld JOIN tax_lots tl ON tl.id = ld.lot_id
		WHERE ld.proceeds_status = 'pending'
	`)
	if err != nil {
		log.Fatalf("query pending: %v", err)
	}
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.asset, &t.at); err != nil {
			log.Fatalf("scan: %v", err)
		}
		if !seen[t] {
			seen[t] = true
			targets = append(targets, t)
		}
	}
	rows.Close()

	var resolved, uncovered int
	for _, t := range targets {
		hp, src, err := reader.Historical(ctx, t.asset, t.at)
		if err != nil {
			// No stored point covers this moment — stays pending, by design.
			uncovered++
			continue
		}
		resolved++
		if *dry {
			continue
		}
		if err := hook(ctx, t.asset, t.at, hp.PriceUSD, ledger.CostBasisFMVAtTransfer); err != nil {
			log.Fatalf("resolve %s @ %s: %v", t.asset, t.at, err)
		}
		_ = src
	}

	fmt.Printf("targets=%d resolved_from_local_history=%d left_pending_no_covering_point=%d dry_run=%v\n",
		len(targets), resolved, uncovered, *dry)
}
