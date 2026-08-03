// Command reconcile-report answers whether the ledger's balances add up
// against what the blockchain provider reports (issue #61, decision #41 as
// amended by #49).
//
// "Adds up" means the RED category is empty. Not a number, not a percentage.
//
// It reads the database and the provider's RAW JSON, sorts every position into
// an explained category, and exits non-zero when anything is left unexplained:
//
//	exit 0 — the red category is empty
//	exit 1 — there are red rows; the report is still printed IN FULL
//	exit 2 — a check could not be run, so no verdict was reached
//
// 1 and 2 are separated deliberately: "did not add up" and "was not checked"
// are different answers. When the provider is unreachable the command does NOT
// fail as a whole — the three checks that need no network still run and still
// report, and only the verdict is marked not-run. Losing the most reliable
// checks to the least reliable one is the failure this split prevents.
//
// # Usage
//
//	# live: one provider call per chain, then the full report
//	reconcile-report -wallet <uuid>
//
//	# capture a snapshot and report on it in the same run
//	reconcile-report -wallet <uuid> -save-snapshot docs/reconcile/snap.json
//
//	# replay a snapshot: no provider calls at all, byte-identical between runs
//	reconcile-report -wallet <uuid> -snapshot docs/reconcile/snap.json
//
//	# skip the provider entirely: the three networkless checks, exit 2
//	reconcile-report -wallet <uuid> -no-provider
//
// The snapshot is the SECOND ENTRANCE of this same command, not a different
// tool: the provider budget is small while a report under development is run
// dozens of times, acceptance has to be repeatable on the same data, and a
// frozen P is what makes two runs diffable.
//
// # Why this is not a phase of sync
//
// The checker must not be the checked. In sync, P would arrive through the same
// contract normalization the ledger side uses, so a single mistake would appear
// on both sides and the reconciliation would agree with itself. Running a sync
// before the report is worse still: sync calls the reconciliation that used to
// top the ledger up TO the positions, after which P↔L agrees because it was made
// to. This command therefore syncs nothing and writes nothing.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/noves"
	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/platform/reconcilereport"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/config"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		walletID    = flag.String("wallet", "", "wallet UUID to reconcile (required)")
		snapshotIn  = flag.String("snapshot", "", "read positions from this snapshot file instead of the provider")
		snapshotOut = flag.String("save-snapshot", "", "write the provider's raw response to this file")
		noProvider  = flag.Bool("no-provider", false, "skip the provider entirely; run only the three networkless checks")
		quietTable  = flag.Bool("no-table", false, "suppress the human-readable table on stderr")
		probeCursor = flag.Bool("probe-cursor", false,
			"additionally ask the provider whether it holds transactions newer than the collection cursor "+
				"(one extra call per chain; the answer is stored in the snapshot)")
	)
	flag.Parse()

	if *walletID == "" {
		fmt.Fprintln(os.Stderr, "reconcile-report: -wallet is required")
		flag.Usage()
		return 2
	}
	wid, err := uuid.Parse(*walletID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reconcile-report: invalid -wallet: %v\n", err)
		return 2
	}

	ctx := context.Background()

	// Logs go to stderr so stdout carries the JSON and nothing else. A log line
	// on stdout would break the byte-identical property two runs depend on.
	log := logger.New("production", os.Stderr)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "reconcile-report: failed to load config: %v\n", err)
		return 2
	}

	db, err := postgres.NewPool(ctx, postgres.Config{URL: cfg.DatabaseURL})
	if err != nil {
		fmt.Fprintf(os.Stderr, "reconcile-report: failed to connect to database: %v\n", err)
		return 2
	}
	defer db.Close()

	in, err := gather(ctx, gatherDeps{
		pool:        db,
		log:         log,
		novesAPIKey: cfg.NovesAPIKey,
		walletID:    wid,
		snapshotIn:  *snapshotIn,
		snapshotOut: *snapshotOut,
		noProvider:  *noProvider,
		probeCursor: *probeCursor,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "reconcile-report: %v\n", err)
		return 2
	}

	report := reconcilereport.Build(*in)

	if err := reconcilereport.WriteJSON(os.Stdout, report); err != nil {
		fmt.Fprintf(os.Stderr, "reconcile-report: %v\n", err)
		return 2
	}
	if !*quietTable {
		reconcilereport.WriteTable(os.Stderr, report)
	}

	return report.ExitCode()
}

type gatherDeps struct {
	pool        *postgres.DB
	log         *logger.Logger
	novesAPIKey string
	walletID    uuid.UUID
	snapshotIn  string
	snapshotOut string
	noProvider  bool
	probeCursor bool
}

// gather assembles the report's Input: the ledger side, the flow side and the
// provider side.
//
// A failure to load the LEDGER side is fatal — without it there is no report at
// all. A failure to reach the PROVIDER is not: it degrades the run to the three
// networkless checks plus exit 2, which is the whole point of separating "did
// not add up" from "was not checked".
func gather(ctx context.Context, d gatherDeps) (*reconcilereport.Input, error) {
	walletRepo := postgres.NewWalletRepository(d.pool.Pool)
	w, err := walletRepo.GetByID(ctx, d.walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet: %w", err)
	}
	if w == nil {
		return nil, fmt.Errorf("wallet %s not found", d.walletID)
	}

	reportRepo := postgres.NewReconcileReportRepository(d.pool.Pool)
	ledgerSnap, err := reportRepo.Load(ctx, d.walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to load ledger side: %w", err)
	}

	knownness, err := reportRepo.KnownnessVerdicts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load knownness verdicts: %w", err)
	}

	// F comes from sync.NetFlows (#60) — the SAME function the reconciler calls,
	// with the same rejection predicate. There is deliberately no second
	// implementation of F: the report and the per-chain sync flag must explain
	// one fact identically, and that is guaranteed by shared code rather than by
	// agreement between two authors.
	rawRepo := postgres.NewRawTransactionRepository(d.pool.Pool)
	raws, err := rawRepo.GetAllByWallet(ctx, d.walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to load raw transactions: %w", err)
	}
	knownnessRepo := postgres.NewAssetKnownnessRepository(d.pool.Pool)
	flows, err := sync.NetFlows(ctx, raws, sync.NewKnownAssetFilter(knownnessRepo), d.log)
	if err != nil {
		return nil, fmt.Errorf("failed to compute net flows: %w", err)
	}

	// F is computed over ALL collected raws while L holds only what posted, so
	// the transactions that never posted are part of every F↔L delta. Their
	// counts are carried so the diagnosis names its own cause instead of
	// reporting a symptom the database can already explain.
	unposted := map[string]int{}
	for _, raw := range raws {
		switch raw.ProcessingStatus {
		case sync.ProcessingStatusProcessed:
		default:
			unposted[string(raw.ProcessingStatus)]++
		}
	}

	in := &reconcilereport.Input{
		WalletID:      d.walletID.String(),
		WalletAddress: w.Address,
		Ledger:        ledgerSnap,
		Flows:         flows,
		Knownness:     knownness,
		UnpostedRaws:  unposted,
	}

	chains, err := walletRepo.GetChainSyncRows(ctx, d.walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to load wallet chain set: %w", err)
	}

	switch {
	case d.noProvider:
		in.PositionsAvailable = false
		in.PositionsSource = "none (-no-provider)"
		in.PositionsUnavailableReason = "the provider was not consulted (-no-provider)"

	case d.snapshotIn != "":
		snap, err := reconcilereport.LoadSnapshot(d.snapshotIn)
		if err != nil {
			// A broken snapshot is "could not check", not "did not add up" — the
			// same boundary exit 2 draws everywhere else.
			in.PositionsAvailable = false
			in.PositionsSource = "snapshot " + d.snapshotIn
			in.PositionsUnavailableReason = err.Error()
			break
		}
		positions, err := snap.Positions()
		if err != nil {
			in.PositionsAvailable = false
			in.PositionsSource = "snapshot " + d.snapshotIn
			in.PositionsUnavailableReason = err.Error()
			break
		}
		in.Positions = positions
		in.PositionsAvailable = true
		in.PositionsSource = "snapshot " + d.snapshotIn
		in.PositionsFetchedAt = snap.CapturedAt
		in.NewerThanCursor = snap.NewerThanCursor
		in.CursorProbed = snap.CursorProbed

	default:
		snap, err := fetchSnapshot(ctx, d, w.Address, chains)
		if err != nil {
			d.log.Warn("provider unavailable; the two P-dependent checks will be marked not run",
				"error", err)
			in.PositionsAvailable = false
			in.PositionsSource = "provider (unavailable)"
			in.PositionsUnavailableReason = err.Error()
			break
		}
		// Saved BEFORE normalization, deliberately. A response the report cannot
		// parse is the one most worth freezing — it is the evidence for the bug —
		// and saving after a successful parse would discard exactly those bytes,
		// leaving nothing to reproduce the failure from and forcing another trip
		// to a provider whose budget is the reason snapshots exist.
		if d.snapshotOut != "" {
			if serr := reconcilereport.SaveSnapshot(d.snapshotOut, snap); serr != nil {
				return nil, fmt.Errorf("failed to save snapshot: %w", serr)
			}
			d.log.Info("snapshot saved", "path", d.snapshotOut)
		}
		positions, perr := snap.Positions()
		if perr != nil {
			in.PositionsAvailable = false
			in.PositionsSource = "provider (unreadable)"
			in.PositionsUnavailableReason = perr.Error()
			break
		}
		in.Positions = positions
		in.PositionsAvailable = true
		in.PositionsSource = "provider"
		in.PositionsFetchedAt = snap.CapturedAt
		in.NewerThanCursor = snap.NewerThanCursor
		in.CursorProbed = snap.CursorProbed
	}

	return in, nil
}

// fetchSnapshot takes the provider's raw balance response for every chain in the
// wallet's chain set.
//
// It is ONE call per chain and nothing else. The command never asks for
// transaction history: the ledger side is what the pipeline already collected,
// and re-collecting it here would make the report a sync, which is what #41
// forbids.
//
// A per-chain failure fails the whole fetch rather than yielding a partial P.
// A partial P would silently turn every position on the missing chain into a
// row the ledger holds and the provider does not — a green category — so a
// transport failure would read as a clean result.
func fetchSnapshot(
	ctx context.Context,
	d gatherDeps,
	address string,
	chains []wallet.WalletChainSync,
) (*reconcilereport.Snapshot, error) {
	if d.novesAPIKey == "" {
		return nil, errors.New("NOVES_API_KEY is not set")
	}
	client := noves.NewClient(d.novesAPIKey, d.log)

	snap := &reconcilereport.Snapshot{
		WalletAddress: address,
		CapturedAt:    time.Now().UTC(),
		Chains:        map[string][]reconcilereport.RawBalance{},
		CursorProbed:  d.probeCursor,
	}

	rows := append([]wallet.WalletChainSync(nil), chains...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Chain < rows[j].Chain })

	for _, row := range rows {
		slug, ok := noves.ChainSlug(row.Chain)
		if !ok {
			// NOT a skip. A chain in the wallet's set that the provider cannot be
			// asked about yields a partial P, and a partial P turns every ledger
			// holding on that chain into a row the provider merely fails to
			// report — a GREEN category. The report would then exit 0 on a wallet
			// it never checked. Refusing the whole fetch keeps "was not checked"
			// where it belongs, in exit 2.
			return nil, fmt.Errorf(
				"chain %q is in the wallet's chain set but has no provider slug: "+
					"a partial P would silently turn its ledger holdings into a green category",
				row.Chain)
		}
		body, err := client.GetBalancesRaw(ctx, slug, address)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch balances for %s: %w", row.Chain, err)
		}
		var balances []reconcilereport.RawBalance
		if err := json.Unmarshal(body, &balances); err != nil {
			return nil, fmt.Errorf("failed to decode balances for %s: %w", row.Chain, err)
		}
		snap.Chains[row.Chain] = balances

		if !d.probeCursor {
			continue
		}
		newer, err := hasNewerThanCursor(ctx, client, slug, address, row.CollectCursorAt)
		if err != nil {
			return nil, fmt.Errorf("failed to probe cursor for %s: %w", row.Chain, err)
		}
		if newer {
			snap.NewerThanCursor = append(snap.NewerThanCursor, row.Chain)
		}
	}

	if len(snap.Chains) == 0 {
		return nil, errors.New("no chain produced positions")
	}
	return snap, nil
}

// hasNewerThanCursor asks whether the provider holds any transaction dated after
// the chain's collection cursor.
//
// It stops at the FIRST page: the question is existence, not how many, and
// paginating to the end would spend the provider budget proving something the
// first row already settles. errEnough is the deliberate early exit
// StreamTransactions documents, not a failure.
//
// The provider's startTimestamp is INCLUSIVE, so the cursor transaction itself
// comes back and must not be counted as newer. Comparing mined times rather than
// counting rows is what keeps a re-seen boundary transaction from reading as
// incomplete collection on every single run.
func hasNewerThanCursor(
	ctx context.Context,
	client *noves.Client,
	slug, address string,
	cursor *time.Time,
) (bool, error) {
	if cursor == nil {
		// Nothing was ever collected on this chain, so "newer than the cursor" is
		// not a question that can be asked; the ledger being empty is visible in
		// the rows themselves.
		return false, nil
	}

	found := false
	errEnough := errors.New("first page is enough")
	err := client.StreamTransactions(ctx, slug, address, *cursor, func(page []noves.Transaction) error {
		for _, tx := range page {
			if tx.RawTransactionData.Timestamp > cursor.Unix() {
				found = true
				break
			}
		}
		return errEnough
	})
	if err != nil && !errors.Is(err, errEnough) {
		return false, err
	}
	return found, nil
}
