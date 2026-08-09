package reconcilereport

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"

	"github.com/kislikjeka/moontrack/pkg/money"
)

// WriteJSON renders the report as the machine-readable answer on stdout.
//
// It is DETERMINISTIC by construction: rows are sorted by (chain, contract),
// maps are marshalled by Go with sorted keys, and no value anywhere is the
// moment the report ran. Two runs over one snapshot therefore produce
// byte-identical bytes, which is what makes diffing two runs — the main way this
// check is used in acceptance — possible at all.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("failed to encode report: %w", err)
	}
	return nil
}

// WriteTable renders the human-readable answer on stderr, in four blocks — one
// per check — followed by the attributed rows.
//
// Two streams rather than one: the JSON must stay diffable byte for byte, and a
// table interleaved into it would make that impossible. stderr also means a
// caller can pipe the verdict into a file and still watch the table.
func WriteTable(w io.Writer, r Report) {
	fmt.Fprintf(w, "RECONCILIATION REPORT — wallet %s (%s)\n", r.WalletID, r.WalletAddress)
	fmt.Fprintf(w, "positions from: %s", r.PositionsSource)
	if r.PositionsFetchedAt != "" {
		fmt.Fprintf(w, " (taken %s)", r.PositionsFetchedAt)
	}
	fmt.Fprintln(w)

	// The two moments whose gap is printed rather than closed.
	chains := make([]string, 0, len(r.SyncedUntil))
	for c := range r.SyncedUntil {
		chains = append(chains, c)
	}
	sort.Strings(chains)
	for _, c := range chains {
		fmt.Fprintf(w, "synced until:   %-10s %s\n", c, r.SyncedUntil[c])
	}
	if !r.CursorProbed {
		fmt.Fprintln(w, "cursor probe:   NOT ASKED — the provider was not asked whether it holds "+
			"transactions newer than the cursor (-probe-cursor)")
	}
	fmt.Fprintln(w)

	for i, c := range r.Checks {
		fmt.Fprintf(w, "── CHECK %d/%d: %s — %s\n", i+1, len(r.Checks), c.Name, c.Status)
		if c.Status == CheckNotRun {
			fmt.Fprintf(w, "     not run: %s\n", c.NotRunReason)
			fmt.Fprintln(w)
			continue
		}
		if c.PartialReason != "" {
			fmt.Fprintf(w, "     partial: %s (the F↔L diagnosis edge still ran)\n", c.PartialReason)
		}
		if len(c.Findings) == 0 {
			fmt.Fprintln(w, "     no findings")
			fmt.Fprintln(w)
			continue
		}
		for _, f := range c.Findings {
			fmt.Fprintf(w, "     ✗ %s\n", f)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "── ROWS (%d)\n", len(r.Rows))
	fmt.Fprintf(w, "%-9s %-44s %-24s %-32s %20s %20s %20s %20s\n",
		"CHAIN", "CONTRACT", "SYMBOL", "CATEGORY", "PROVIDER", "FLOW", "LEDGER", "REJECTED")
	for _, row := range r.Rows {
		mark := " "
		if row.Category.IsRed() {
			mark = "✗"
		}
		fmt.Fprintf(w, "%s%-8s %-44s %-24s %-32s %20s %20s %20s %20s\n",
			mark, row.Chain, row.Contract, truncate(row.Symbol, 24), row.Category,
			human(row.Provider, row.Decimals), human(row.Flow, row.Decimals),
			human(row.Ledger, row.Decimals), human(row.RejectedAmount, row.Decimals))
	}
	fmt.Fprintln(w)

	cats := make([]string, 0, len(r.Summary.ByCategory))
	for c := range r.Summary.ByCategory {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	fmt.Fprintln(w, "── SUMMARY")
	for _, c := range cats {
		fmt.Fprintf(w, "     %-34s %d\n", c, r.Summary.ByCategory[c])
	}
	fmt.Fprintf(w, "     %-34s %d\n", "check findings", r.Summary.Findings)
	fmt.Fprintf(w, "     %-34s %d\n", "checks not run", r.Summary.ChecksNotRun)
	fmt.Fprintln(w)

	// The verdict, stated as the ticket states it: "adds up" is the red category
	// being EMPTY, never a number and never a percentage.
	switch r.ExitCode() {
	case 0:
		fmt.Fprintln(w, "VERDICT: the red category is EMPTY — every difference is accounted for.")
	case 1:
		fmt.Fprintf(w, "VERDICT: DOES NOT ADD UP — %d red row(s), %d check finding(s).\n",
			r.Summary.Red, r.Summary.Findings)
	case 2:
		fmt.Fprintf(w, "VERDICT: NOT ESTABLISHED — %d check(s) could not run. "+
			"%d red row(s) were found by the checks that did.\n",
			r.Summary.ChecksNotRun, r.Summary.Red)
	}
}

// human renders a base-unit quantity at its decimals, for the table only. The
// JSON keeps base units untouched: a rendered amount is for reading, never for
// comparing, and the comparison already happened.
//
// Negative quantities render as-is: money.FromBaseUnits handles the sign since
// #71. A negative ledger balance is a real state worth reading correctly — it is
// what an over-reported outflow looks like.
func human(baseUnits string, decimals int) string {
	if baseUnits == "" {
		return "—"
	}
	n, ok := new(big.Int).SetString(baseUnits, 10)
	if !ok || decimals <= 0 {
		return baseUnits
	}
	return truncate(money.FromBaseUnits(n, decimals), 20)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
