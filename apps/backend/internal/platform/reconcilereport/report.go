package reconcilereport

import (
	"math/big"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Category is the attribution attached to every row of the P∪L union.
//
// Attribution replaces a threshold, and the reason is that the difference is
// structurally made of EXPLAINABLE PIECES rather than noise around zero. A
// percentage answers the wrong question in both directions: a dropped leg of a
// cheap token is 0.01% and would pass any threshold, while spam carrying a fake
// price is 300% and would fail one. So each position is named and attributed,
// and the verdict is whether anything is left unattributed.
type Category string

const (
	// CategoryAgrees — present on both sides, quantities equal within tolerance.
	CategoryAgrees Category = "agrees"

	// CategoryExplainedReceipt — in P, absent from L because the receipt rule
	// (#57) kept the leg out. GREEN. A protocol receipt is OBLIGED to appear in
	// P and to be missing from L: the principal it was minted against is already
	// booked, and booking the receipt beside it counts one supply twice.
	CategoryExplainedReceipt Category = "explained_receipt"

	// CategoryExplainedUnknownAsset — in P, absent from L because the known-asset
	// filter (#58) rejected the leg with a verdict. GREEN, but listed BY NAME
	// with contract, symbol and quantity: what the filter dropped may be spam or
	// may be a broken resolve, and only the name distinguishes them.
	CategoryExplainedUnknownAsset Category = "explained_unknown_asset"

	// CategoryCheckFailed — in P, absent from L because the asset has no
	// knownness verdict YET. GREEN, and deliberately not merged with the
	// previous one: "checked, and the answer is no" and "nobody has managed to
	// check" are different facts with different remedies, and #58 went to some
	// trouble to keep them apart.
	CategoryCheckFailed Category = "check_failed"

	// CategoryUnexplainedMissingFromLedger — in P, absent from L, and NOTHING
	// accounts for it. RED.
	//
	// This is the whole of the amendment from #49. Decision #41 originally made
	// the entire "in P, not in L" category red; after genesis was switched off
	// that category fills up as a matter of course on every wallet with DeFi or
	// spam, so a report that reddened on it would be red always and would stop
	// meaning anything. Only the unattributed remainder is a finding.
	CategoryUnexplainedMissingFromLedger Category = "unexplained_missing_from_ledger"

	// CategoryAmountMismatch — present on both sides, quantities disagree beyond
	// the per-position tolerance. RED.
	CategoryAmountMismatch Category = "amount_mismatch"

	// CategoryLedgerOnly — held in the ledger, not reported by the provider.
	// GREEN, and printed: a DeFi wrapper the balances endpoint cannot see looks
	// exactly like this, and so does an asset spent down to a residue.
	CategoryLedgerOnly Category = "ledger_only"

	// CategorySettled — absent from P and holding nothing in the ledger. NOT
	// PRINTED: both sides agree there is no position, and printing every asset
	// the wallet ever spent to zero, plus every spam token the filter kept out
	// entirely, would drown the rows that mean something.
	CategorySettled Category = "settled"
)

// IsRed reports whether a category is a finding rather than an explanation.
func (c Category) IsRed() bool {
	return c == CategoryUnexplainedMissingFromLedger || c == CategoryAmountMismatch
}

// positionTolerance is the per-position allowance, in BASE UNITS, inside the
// red categories.
//
// It guards against honest micro-deltas, not against significance. A rebase
// token (stETH) and any interest-accruing position drift by a few base units
// continuously between the moment the history was collected and the moment the
// balance was read; without an allowance the report would be red permanently
// and would stop being read. It matches the reconciler's own dust tolerance so
// the report and the per-chain flag do not disagree about a handful of units.
//
// It is emphatically NOT a percentage: see Category.
var positionTolerance = big.NewInt(10)

// Row is one asset identity in the report: the three sides of the triangle plus
// the attribution that says which of them are allowed to disagree.
//
// Field order and JSON names are fixed, and the row carries NO timestamp.
// Timestamps belong to the run, not to the facts, and a timestamp inside a row
// would make two runs impossible to diff — which is the main way this report is
// used.
type Row struct {
	Chain    string   `json:"chain"`
	Contract string   `json:"contract"`
	Symbol   string   `json:"symbol"`
	Decimals int      `json:"decimals"`
	Category Category `json:"category"`

	// Provider, Flow and Ledger are P, F and L in base units, rendered as
	// decimal strings because a base-unit quantity does not fit a JSON number
	// and must never be rounded through a float. Empty string means the side has
	// no entry for this identity at all — distinct from "0", which means it has
	// one and it is zero.
	Provider string `json:"provider"`
	Flow     string `json:"flow"`
	Ledger   string `json:"ledger"`

	// Delta is P − L in base units, present only when both sides have a value.
	Delta string `json:"delta,omitempty"`

	// FlowLedgerDelta is F − L: the DIAGNOSIS edge. Both sides are built from the
	// same collected history, so they can differ only in the POSTING. A non-zero
	// value here says the defect is in how the ledger was written, and P need not
	// be consulted at all.
	FlowLedgerDelta string `json:"flow_ledger_delta,omitempty"`

	// RejectedBy names every rule that kept this asset's legs out of the ledger,
	// which is what turns "missing from L" from a finding into an explanation.
	RejectedBy []string `json:"rejected_by,omitempty"`

	// RejectedAmount is the total base-unit magnitude the rules kept out of the
	// ledger for this asset, summed over the collected history.
	//
	// The SIZE is required, not decoration: #41 asks for the filtered assets
	// поимённо WITH quantities, because a name alone cannot tell spam from a
	// broken resolve — a dust airdrop and a real holding wrongly filtered look
	// identical without it. It is separate from Provider because they answer
	// different questions: Provider is the position now, RejectedAmount is how
	// much the history tried to book and was refused.
	RejectedAmount string `json:"rejected_amount,omitempty"`

	// KnownnessStatus is the stored verdict for the asset, carried so a green
	// "check_failed" row can be told from a green "explained" one at a glance.
	KnownnessStatus string `json:"knownness_status,omitempty"`
}

// CheckStatus is whether a check produced an answer.
//
// "Failed" is not among the values: a check either ran and found things, or
// could not run. That distinction is the same one exit codes 1 and 2 draw, and
// it exists because "did not add up" and "we did not check" are different
// answers that a single boolean would merge.
type CheckStatus string

const (
	// CheckRan — the check executed and its findings are complete.
	CheckRan CheckStatus = "ran"

	// CheckNotRun — the check could not execute; its findings say nothing.
	CheckNotRun CheckStatus = "not_run"
)

// Check is one of the four checks, with its own findings and its own status.
//
// Four rather than one because they differ in KIND, not in strictness. The
// triangle compares magnitudes; materialization compares two statements of one
// magnitude; account shape compares nothing at all and counts instead; the
// verdict is the destination. A single pass would have to pick one of those
// natures and would lose the others.
type Check struct {
	Name   string      `json:"name"`
	Status CheckStatus `json:"status"`

	// NotRunReason states why, empty when the check ran. A check silently
	// marked not-run would be worse than a failing one.
	NotRunReason string `json:"not_run_reason,omitempty"`

	// PartialReason names a part of the check that could not be performed while
	// the rest of it did. It exists for the triangle: its F↔L edge is built from
	// the database alone and stays valid when the provider is unreachable, while
	// its P column does not. Collapsing the two into "not run" would discard a
	// working diagnosis because an unrelated input was missing; collapsing them
	// into "ran" would overstate what was checked.
	PartialReason string `json:"partial_reason,omitempty"`

	// Findings are the check's own red lines, in fixed order.
	Findings []string `json:"findings"`
}

// Report is the whole answer: four checks, the attributed rows, and the two
// moments whose gap the report prints rather than closes.
type Report struct {
	WalletID      string `json:"wallet_id"`
	WalletAddress string `json:"wallet_address"`

	// SyncedUntil maps chain to the moment collection reached, and
	// PositionsFetchedAt is when the balances were taken. The gap between them is
	// real and is NOT removed by running a sync first — doing that would call the
	// reconciliation that used to top the ledger up TO P, after which P↔L agrees
	// because it was made to. So the gap is printed and left visible.
	//
	// Both are inputs read from the database and the snapshot, never "now", so
	// they do not break the byte-identical property of two runs.
	SyncedUntil        map[string]string `json:"synced_until"`
	PositionsFetchedAt string            `json:"positions_fetched_at"`

	// PositionsSource says where P came from: a snapshot path, or the live
	// provider. Without it, a green report cannot be told from a green report
	// run against yesterday's snapshot.
	PositionsSource string `json:"positions_source"`

	// CursorProbed says whether the provider was asked about transactions newer
	// than the collection cursor. False means the question was not asked, which
	// the report states plainly rather than letting an empty answer read as a
	// clean one.
	CursorProbed bool `json:"cursor_probed"`

	Checks []Check `json:"checks"`

	// Rows are sorted by (chain, contract) — never by map order, which would make
	// two runs differ for no reason.
	Rows []Row `json:"rows"`

	Summary Summary `json:"summary"`
}

// Summary counts the rows by verdict. It is a convenience for reading, never
// the verdict itself: the verdict is whether Red is zero, and no other number
// here means anything on its own.
type Summary struct {
	Red        int            `json:"red"`
	Green      int            `json:"green"`
	ByCategory map[string]int `json:"by_category"`

	// Findings counts the checks' own red lines, which are NOT rows: a stale
	// materialization and a split account are properties of the ledger's shape
	// and have no asset row to hang off.
	Findings int `json:"findings"`

	ChecksNotRun int `json:"checks_not_run"`
}

// RedRows returns the rows that are findings.
func (r Report) RedRows() []Row {
	var out []Row
	for _, row := range r.Rows {
		if row.Category.IsRed() {
			out = append(out, row)
		}
	}
	return out
}

// ExitCode is the report's verdict as a process exit code.
//
//	0 — the red category is empty
//	1 — there are red rows (the report is still printed IN FULL)
//	2 — a check could not be run, so no verdict is available
//
// 2 dominates 1 deliberately. "Did not add up" and "was not checked" are
// different claims, and a command that reported 1 while one of its checks never
// ran would be asserting a verdict it did not reach. Red rows found by the
// checks that DID run are still printed — the point of not failing the whole
// command when the provider is down is that the three reliable checks are not
// lost to the one unreliable one.
func (r Report) ExitCode() int {
	for _, c := range r.Checks {
		if c.Status == CheckNotRun {
			return 2
		}
	}
	// A finding on ANY of the four checks is red, not only a red ROW. Three of
	// the checks produce no rows at all — a stale materialization and a split
	// account are properties of the ledger's shape, not of an asset — so a
	// verdict read from rows alone would report success on exactly the failures
	// checks 2 and 3 exist to find.
	if len(r.RedRows()) > 0 || r.hasFindings() {
		return 1
	}
	return 0
}

// hasFindings reports whether any check found something.
func (r Report) hasFindings() bool {
	for _, c := range r.Checks {
		if len(c.Findings) > 0 {
			return true
		}
	}
	return false
}

// keyOf is the sort key of a row, matching the report's fixed ordering.
func keyOf(k sync.AssetKey) (string, string) { return k.Chain, k.Contract }
