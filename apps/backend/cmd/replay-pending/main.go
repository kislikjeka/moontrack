// Command replay-pending processes a wallet's already-collected raw
// transactions through the real ledger path, WITHOUT contacting any provider.
//
// It exists to answer the question #77 turns on: the nil-price dereference that
// segfaulted the backend mid-resync (defi.generateSwapLikeEntries calling
// (*big.Int).Sign on an unknown price) is fixed and covered by unit tests, but
// unit tests choose their own inputs. The transactions that actually killed the
// process are still sitting in the database as `processing_status='pending'` —
// stranded exactly where the panic left them. Only replaying THOSE proves the
// fix.
//
// # Why no provider is needed
//
// Sync is three phases: collect (provider → raw_transactions), process
// (raw_transactions → ledger), reconcile (provider → balances). The panic was
// in phase two, and phase two reads the database only:
// Processor.ProcessAll takes pending raws from rawTxRepo.GetPendingByWallet and
// walks them through TxBuilder into ledger.RecordTransaction. Nothing in that
// path calls Noves.
//
// sync.NewService constructs the processor only when a txProvider is present,
// which is what makes this command necessary rather than merely convenient:
// NewProcessor and NewTxBuilder themselves take no provider, so they are wired
// here directly and the collector is never built at all. There is therefore no
// code path in this binary that can reach the network.
//
// # Why it mirrors cmd/api/main.go instead of simplifying
//
// The point is to exercise the real path, not a re-implementation of it. Every
// handler the API registers is registered here, in the same order, with the
// same tax lot hook installed on the same ledger service. A shorter wiring that
// registered only the DeFi handlers would prove something about a program that
// does not exist: the raw set spans receive, execute, trade, send, approve,
// deposit and withdraw, and it is the interaction of the whole registry with
// real data that has to survive.
//
// The price backfill job enqueuer IS wired, because an unpriced leg enqueuing a
// job is part of the behaviour under test. Nothing here drains that queue — the
// worker in the running backend does that, and this command deliberately owns
// no worker.
//
// # Two scopes: the stranded tail, or the whole wallet
//
// By default the command processes whatever is already pending, which is the
// scope #77 needs: the crash left those raws behind and only they have to be
// proven bookable.
//
// -wipe widens it to the whole wallet. It calls wipe_wallet_ledger first, which
// deletes what this wallet's raws produced and returns every one of them to
// 'pending', so the run re-derives the wallet from its raws instead of topping
// up a tail. That is the scope acceptance needs (#85): a defect fixed in the
// booking logic — #70's cross-chain account split, say — leaves already-booked
// transactions wrong, and only a wipe-and-replay re-derives them.
//
// Re-deriving is sound because the decision is a pure function of the collected
// raws (ADR-0002), which is also why neither scope needs the provider.
//
// # Usage
//
//	replay-pending -wallet <uuid> -dry-run   # report the pending set, write nothing
//	replay-pending -wallet <uuid>            # process the pending set for real
//	replay-pending -wallet <uuid> -wipe      # reset the wallet, then re-derive all of it
//
// Exit codes: 0 the run completed (raws that failed individually are recorded
// on the raw as errors, which is the existing behaviour and not a failure of
// the run); 1 the run could not be completed at all.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/adjustment"
	"github.com/kislikjeka/moontrack/internal/module/defi"
	"github.com/kislikjeka/moontrack/internal/module/genesis"
	"github.com/kislikjeka/moontrack/internal/module/lending"
	"github.com/kislikjeka/moontrack/internal/module/liquidity"
	"github.com/kislikjeka/moontrack/internal/module/swap"
	"github.com/kislikjeka/moontrack/internal/module/transfer"
	"github.com/kislikjeka/moontrack/internal/platform/lendingposition"
	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "postgres DSN")
	walletID := flag.String("wallet", "", "wallet UUID whose pending raws to replay")
	dry := flag.Bool("dry-run", false, "report the pending set and write nothing")
	wipe := flag.Bool("wipe", false, "reset the wallet's ledger first, re-pending every raw (see wipe_wallet_ledger)")
	flag.Parse()

	if *dsn == "" {
		fatal("missing -dsn (or DATABASE_URL)")
	}
	if *walletID == "" {
		fatal("missing -wallet")
	}
	wID, err := uuid.Parse(*walletID)
	if err != nil {
		fatal("bad -wallet: %v", err)
	}

	ctx := context.Background()
	db, err := postgres.NewPool(ctx, postgres.Config{URL: *dsn})
	if err != nil {
		fatal("connect: %v", err)
	}
	defer db.Close()

	log := logger.NewDefault("development")

	// --- Repositories -------------------------------------------------------
	ledgerRepo := postgres.NewLedgerRepository(db.Pool)
	walletRepo := postgres.NewWalletRepository(db.Pool)
	taxLotRepo := postgres.NewTaxLotRepository(db.Pool)
	rawTxRepo := postgres.NewRawTransactionRepository(db.Pool)
	assetRegistryRepo := postgres.NewAssetRegistryRepository(db.Pool)
	priceBackfillJobRepo := postgres.NewPriceBackfillJobRepository(db.Pool)
	knownnessRepo := postgres.NewAssetKnownnessRepository(db.Pool)

	// --- Ledger core + tax lot hook ----------------------------------------
	handlerRegistry := ledger.NewRegistry()
	ledgerSvc := ledger.NewService(ledgerRepo, handlerRegistry, log)
	ledgerSvc.RegisterPostBalanceHook(ledger.NewTaxLotHook(taxLotRepo, ledgerRepo, log))

	// --- Handlers, in cmd/api/main.go's order -------------------------------
	handlerRegistry.Register(adjustment.NewAssetAdjustmentHandler(ledgerSvc, log))
	handlerRegistry.Register(transfer.NewTransferInHandler(walletRepo, log))
	handlerRegistry.Register(transfer.NewTransferOutHandler(walletRepo, log))
	handlerRegistry.Register(transfer.NewInternalTransferHandler(walletRepo, log))
	handlerRegistry.Register(swap.NewSwapHandler(walletRepo, log))
	handlerRegistry.Register(defi.NewDeFiDepositHandler(walletRepo, log))
	handlerRegistry.Register(defi.NewDeFiWithdrawHandler(walletRepo, log))
	handlerRegistry.Register(defi.NewDeFiClaimHandler(walletRepo, log))
	handlerRegistry.Register(genesis.NewHandler(log))
	handlerRegistry.Register(liquidity.NewLPDepositHandler(walletRepo, log))
	handlerRegistry.Register(liquidity.NewLPWithdrawHandler(walletRepo, log))
	handlerRegistry.Register(liquidity.NewLPClaimFeesHandler(walletRepo, log))
	handlerRegistry.Register(lending.NewLendingSupplyHandler(walletRepo, log))
	handlerRegistry.Register(lending.NewLendingWithdrawHandler(walletRepo, log))
	handlerRegistry.Register(lending.NewLendingBorrowHandler(walletRepo, log))
	handlerRegistry.Register(lending.NewLendingRepayHandler(walletRepo, log))
	handlerRegistry.Register(lending.NewLendingClaimHandler(walletRepo, log))

	// --- Position services the tx builder writes through --------------------
	lpPositionSvc := lpposition.NewService(postgres.NewLPPositionRepo(db.Pool), log)
	lendingPositionSvc := lendingposition.NewService(postgres.NewLendingPositionRepo(db.Pool), log)

	// The knownness filter reads the local table only and makes no network
	// call, so it behaves here exactly as it does in the API.
	knownFilter := sync.NewKnownAssetFilter(knownnessRepo)

	// Phase two, constructed directly. NewService would refuse to build these
	// without a provider; the provider is what we are proving unnecessary.
	txBuilder := sync.NewTxBuilder(walletRepo, ledgerSvc, lpPositionSvc, lendingPositionSvc, log, priceBackfillJobRepo, assetRegistryRepo, knownFilter)
	processor := sync.NewProcessor(rawTxRepo, walletRepo, txBuilder, log)

	w, err := walletRepo.GetByID(ctx, wID)
	if err != nil {
		fatal("load wallet: %v", err)
	}

	before, err := snapshot(ctx, db, wID)
	if err != nil {
		fatal("snapshot before: %v", err)
	}
	fmt.Println("=== BEFORE ===")
	before.print()

	if *dry {
		fmt.Println("\n=== DRY RUN: nothing processed ===")
		return
	}

	// The reset half of the loop. Without it this command only ever sees the
	// raws that are still pending — which is the right scope for #77 (a crash
	// stranded them there) but the wrong one for re-deriving a wallet whose
	// transactions were already booked from stale logic. wipe_wallet_ledger
	// deletes what this wallet's raws produced and returns those raws to
	// 'pending', so the ProcessAll below re-derives the whole wallet rather
	// than a leftover tail of it.
	//
	// It is internal-transfer-aware (migration 000030): one on-chain event can
	// be a single ledger transaction shared by two of the user's wallets, so
	// the wipe scopes itself by "any raw of this wallet references this
	// transaction" rather than by ownership. Wiping either side therefore
	// reaches the shared transaction and re-pends both sides' raws — which is
	// exactly the case #70 turns on, since both defective accounts came from
	// internal transfers.
	if *wipe {
		fmt.Println("\n=== WIPE ===")
		if err := walletRepo.WipeWalletLedger(ctx, wID); err != nil {
			fatal("wipe: %v", err)
		}
		wiped, err := snapshot(ctx, db, wID)
		if err != nil {
			fatal("snapshot after wipe: %v", err)
		}
		wiped.print()
	}

	fmt.Println("\n=== ProcessAll ===")
	processErr := processor.ProcessAll(ctx, w)
	if processErr != nil {
		// Not fatal on its own: ProcessAll records per-raw failures on the raw
		// and keeps going, so an error here is about the run, not one raw. It
		// is reported and the after-snapshot is still taken, because what the
		// run managed to do before failing is the interesting part.
		fmt.Printf("ProcessAll returned error: %v\n", processErr)
	} else {
		fmt.Println("ProcessAll returned no error")
	}

	after, err := snapshot(ctx, db, wID)
	if err != nil {
		fatal("snapshot after: %v", err)
	}
	fmt.Println("\n=== AFTER ===")
	after.print()

	fmt.Println("\n=== REMAINING ERRORS (this wallet) ===")
	if err := printErrors(ctx, db, wID); err != nil {
		fatal("errors report: %v", err)
	}
}

// counts is the state this command is measuring: how the wallet's raws are
// distributed across processing_status, plus the two downstream tables a
// successful booking writes into.
type counts struct {
	byStatus     map[string]int
	transactions int
	lots         int
}

func (c counts) print() {
	statuses := make([]string, 0, len(c.byStatus))
	for s := range c.byStatus {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)
	for _, s := range statuses {
		fmt.Printf("  raw_transactions %-10s %d\n", s, c.byStatus[s])
	}
	fmt.Printf("  transactions            %d\n", c.transactions)
	fmt.Printf("  tax_lots                %d\n", c.lots)
}

func snapshot(ctx context.Context, db *postgres.DB, walletID uuid.UUID) (counts, error) {
	c := counts{byStatus: map[string]int{}}

	rows, err := db.Pool.Query(ctx,
		`SELECT processing_status, count(*) FROM raw_transactions
		 WHERE wallet_id = $1 GROUP BY 1`, walletID)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return c, err
		}
		c.byStatus[status] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return c, err
	}

	if err := db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM transactions WHERE wallet_id = $1`, walletID,
	).Scan(&c.transactions); err != nil {
		return c, err
	}
	// A lot names no wallet of its own; it reaches one through the transaction
	// that created it.
	if err := db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM tax_lots tl
		 JOIN transactions t ON t.id = tl.transaction_id
		 WHERE t.wallet_id = $1`, walletID,
	).Scan(&c.lots); err != nil {
		return c, err
	}
	return c, nil
}

// printErrors groups the wallet's error rows by message so a repeated failure
// reads as one line with a count rather than as N indistinguishable rows.
func printErrors(ctx context.Context, db *postgres.DB, walletID uuid.UUID) error {
	rows, err := db.Pool.Query(ctx,
		`SELECT coalesce(processing_error, '(null)'), operation_type, count(*)
		 FROM raw_transactions
		 WHERE wallet_id = $1 AND processing_status = 'error'
		 GROUP BY 1, 2 ORDER BY 3 DESC`, walletID)
	if err != nil {
		return err
	}
	defer rows.Close()

	any := false
	for rows.Next() {
		var msg, op string
		var n int
		if err := rows.Scan(&msg, &op, &n); err != nil {
			return err
		}
		any = true
		fmt.Printf("  [%d] %-10s %s\n", n, op, msg)
	}
	if !any {
		fmt.Println("  none")
	}
	return rows.Err()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
