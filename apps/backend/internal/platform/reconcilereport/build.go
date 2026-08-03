package reconcilereport

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Input is everything the report is built from, gathered by the caller so that
// Build itself touches neither the database nor the network.
//
// That is what makes the mandatory tests possible at all: a split account, a
// stale materialization and an unreachable provider are states, and states can
// be constructed but not easily provoked.
type Input struct {
	WalletID      string
	WalletAddress string

	// Ledger is the L side and the sole input of the two networkless checks.
	Ledger *LedgerSnapshot

	// Flows is the F side, produced by sync.NetFlows (#60). It is passed in
	// already computed rather than derived here, so there is exactly one
	// implementation of F in the codebase and the report cannot disagree with
	// the per-chain sync flag about the same asset.
	Flows []sync.AssetNetFlow

	// Knownness maps identity to its stored verdict, used to tell a green
	// "checked: unknown" row from a green "not checked yet" one.
	Knownness map[sync.AssetKey]string

	// Positions is P. Meaningful only when PositionsAvailable is true.
	Positions          []Position
	PositionsAvailable bool
	// PositionsUnavailableReason is why P is missing, printed verbatim.
	PositionsUnavailableReason string
	// PositionsSource records where P came from — a snapshot path or the live
	// provider — so a green report cannot be confused with a green report over
	// stale data.
	PositionsSource string
	// PositionsFetchedAt is when the balances were taken. From the snapshot, or
	// from the moment of the live fetch.
	PositionsFetchedAt time.Time

	// NewerThanCursor names the chains where the provider holds transactions
	// dated after the collection cursor. It is its own red line rather than a
	// per-asset row: it says the ledger side is INCOMPLETE, which invalidates
	// every comparison on that chain rather than any one of them.
	NewerThanCursor []string

	// CursorProbed says whether the question was asked at all. An empty
	// NewerThanCursor with CursorProbed false means "not asked", which the report
	// states rather than silently reading as "no newer transactions" — a
	// reassurance nobody checked is worse than none.
	CursorProbed bool

	// UnpostedRaws counts the collected transactions that never became ledger
	// entries, by processing status.
	//
	// It belongs to the F↔L edge and nowhere else. F is computed over ALL
	// collected raws, because the reconciler's question is "does the collected
	// history account for this balance". L only contains what was successfully
	// posted. So a raw that errored is counted in F and missing from L, and the
	// difference is a real posting defect — but one whose CAUSE is already
	// recorded, and a diagnosis that reports the symptom while the database holds
	// the cause is a diagnosis that sends someone looking in the wrong place.
	UnpostedRaws map[string]int
}

// Build assembles the report. It performs no I/O.
func Build(in Input) Report {
	rows, triangle, verdict := buildRows(in.Ledger, in.Flows, in.Knownness, triangleInput{
		positions:         in.Positions,
		available:         in.PositionsAvailable,
		unavailableReason: in.PositionsUnavailableReason,
	})

	// The time gap between "the history was collected up to here" and "the
	// balances were read now" is NOT closed — closing it would mean running a
	// sync first, and a sync calls the reconciliation that used to top the ledger
	// up TO the positions, after which P↔L agrees because it was made to. So the
	// gap is printed, and the one case where it is actually fatal gets its own
	// red line.
	if in.PositionsAvailable && len(in.NewerThanCursor) > 0 {
		chains := append([]string(nil), in.NewerThanCursor...)
		sort.Strings(chains)
		for _, c := range chains {
			verdict.Findings = append(verdict.Findings,
				"chain "+c+": the provider reports transactions newer than the collection cursor, "+
					"so the ledger side is incomplete and every comparison on this chain is premature")
		}
	}

	// A F↔L finding whose cause is already recorded must SAY so. The triangle
	// reports that the collected history and the ledger disagree; the raws'
	// processing status says why for the commonest case, and leaving the reader
	// to discover it separately would send them hunting a posting bug that is
	// already written down.
	//
	// It is appended as a finding rather than folded into the deltas, because it
	// does not excuse them: a transaction that failed to post IS a hole in the
	// ledger, and the report's job is to make it impossible to overlook.
	if unposted := describeUnposted(in.UnpostedRaws); unposted != "" {
		triangle.Findings = append(triangle.Findings, unposted)
	}

	rep := Report{
		WalletID:           in.WalletID,
		WalletAddress:      in.WalletAddress,
		SyncedUntil:        in.Ledger.ChainCursors,
		PositionsFetchedAt: formatMoment(in.PositionsFetchedAt),
		PositionsSource:    in.PositionsSource,
		CursorProbed:       in.CursorProbed,
		Checks: []Check{
			triangle,
			checkMaterialization(in.Ledger),
			checkAccountShape(in.Ledger),
			verdict,
		},
		Rows: rows,
	}

	if rep.SyncedUntil == nil {
		rep.SyncedUntil = map[string]string{}
	}

	rep.Summary = summarize(rep)
	return rep
}

// summarize counts rows by verdict. The counts are a reading aid; the verdict is
// whether Red is zero.
func summarize(r Report) Summary {
	s := Summary{ByCategory: map[string]int{}}
	for _, row := range r.Rows {
		s.ByCategory[string(row.Category)]++
		if row.Category.IsRed() {
			s.Red++
		} else {
			s.Green++
		}
	}
	for _, c := range r.Checks {
		if c.Status == CheckNotRun {
			s.ChecksNotRun++
		}
		s.Findings += len(c.Findings)
	}
	return s
}

// describeUnposted renders the collected-but-unposted counts as one finding, or
// the empty string when every collected transaction reached the ledger.
//
// `skipped` is counted alongside `error` deliberately. A skip is a decision and
// an error is a failure, but both leave the same hole: the transaction is in F
// and absent from L. Which of the two it was changes the remedy, not whether
// the ledger is short, so the report names both and separates them by status.
func describeUnposted(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	statuses := make([]string, 0, len(counts))
	total := 0
	for s, n := range counts {
		// A posted transaction is not a hole. The status is filtered here as well
		// as at the call site so a caller that hands over the raw tally cannot
		// turn a healthy wallet into a finding.
		if n == 0 || s == string(sync.ProcessingStatusProcessed) {
			continue
		}
		statuses = append(statuses, fmt.Sprintf("%s=%d", s, n))
		total += n
	}
	if total == 0 {
		return ""
	}
	sort.Strings(statuses)
	return fmt.Sprintf(
		"%d collected transaction(s) never reached the ledger (%s). "+
			"They are counted in F and absent from L, so they account for part of "+
			"every flow_ledger_delta above — the cause is already recorded on the raws",
		total, strings.Join(statuses, " "))
}

// formatMoment renders a moment in a fixed, timezone-independent form. Zero time
// renders empty rather than as year 1, which reads as data.
//
// This is applied to INPUT moments only — the sync cursor and the capture time —
// never to "now". Nothing in the report is stamped with the moment it ran: two
// runs over one snapshot must produce byte-identical output, and a run timestamp
// would make the diff that acceptance depends on impossible.
func formatMoment(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
