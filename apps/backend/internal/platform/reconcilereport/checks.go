package reconcilereport

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// Check names. They are stable strings because they appear in the JSON, and the
// JSON is diffed between runs.
const (
	CheckNameTriangle        = "triangle_p_f_l"
	CheckNameMaterialization = "entries_equal_balances"
	CheckNameAccountShape    = "one_account_per_identity"
	CheckNameVerdict         = "provider_vs_ledger"
)

// checkMaterialization compares each account's entries-derived balance with the
// materialized one.
//
// It is a check of its OWN, separate from the triangle, and that separation is
// forced by the decision to take L from the entries. Once L no longer reads
// account_balances, nothing else in the report would ever notice that
// account_balances had gone stale — the triangle would keep agreeing while the
// number every other surface of the application displays was wrong.
//
// NEEDS NO NETWORK: it is a pure function of the ledger snapshot.
func checkMaterialization(s *LedgerSnapshot) Check {
	c := Check{Name: CheckNameMaterialization, Status: CheckRan, Findings: []string{}}

	accounts := append([]LedgerAccount(nil), s.Accounts...)
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Code < accounts[j].Code })

	for _, a := range accounts {
		entries := a.EntriesBalance
		if entries == nil {
			entries = big.NewInt(0)
		}

		// A missing materialized row is only a finding when the entries say the
		// account holds something. An account with no entries and no balance row
		// is simply an account nothing has been posted to yet.
		if a.MaterializedBalance == nil {
			if entries.Sign() != 0 {
				c.Findings = append(c.Findings, fmt.Sprintf(
					"%s: entries=%s but no materialized balance row", a.Code, entries.String()))
			}
			continue
		}

		if entries.Cmp(a.MaterializedBalance) != 0 {
			c.Findings = append(c.Findings, fmt.Sprintf(
				"%s: entries=%s materialized=%s (drift %s)",
				a.Code, entries.String(), a.MaterializedBalance.String(),
				new(big.Int).Sub(entries, a.MaterializedBalance).String()))
		}
	}

	return c
}

// checkAccountShape asserts there is exactly ONE account per (wallet, chain,
// contract).
//
// This is the only check that catches a split account directly, and the reason
// is a property of aggregation rather than a gap in the other checks: once
// balances are summed per identity, two accounts that are jointly correct are
// indistinguishable from one correct account. A split therefore self-heals in
// EVERY comparison of magnitudes — including F↔L, the diagnosis edge — and can
// only be seen by counting the accounts rather than adding them up.
//
// It counts accounts, not non-zero accounts. A split whose second half is
// currently zero is the same defect one posting away from mattering.
//
// NEEDS NO NETWORK: it is a pure function of the ledger snapshot.
// It groups by (scope, chain, contract), where the scope is the account's own
// namespace — the wallet, or one lending protocol. That qualifier is required
// rather than a loosening: `CollateralCode` is protocol-scoped on purpose ("the
// same asset supplied to two protocols is two distinct positions"), so a check
// keyed on the identity alone would fire on correct bookkeeping every time a
// wallet holds an asset and also supplies it. Splitting is two accounts in ONE
// scope, and that is what this counts.
func checkAccountShape(s *LedgerSnapshot) Check {
	c := Check{Name: CheckNameAccountShape, Status: CheckRan, Findings: []string{}}

	type shapeKey struct {
		scope string
		key   sync.AssetKey
	}

	byKey := make(map[shapeKey][]LedgerAccount)
	for _, a := range s.Accounts {
		byKey[shapeKey{scope: accountScope(a), key: a.Key}] = append(
			byKey[shapeKey{scope: accountScope(a), key: a.Key}], a)
	}

	keys := make([]shapeKey, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].key.Chain != keys[j].key.Chain {
			return keys[i].key.Chain < keys[j].key.Chain
		}
		if keys[i].key.Contract != keys[j].key.Contract {
			return keys[i].key.Contract < keys[j].key.Contract
		}
		return keys[i].scope < keys[j].scope
	})

	for _, k := range keys {
		accs := byKey[k]
		if len(accs) <= 1 {
			continue
		}
		codes := make([]string, 0, len(accs))
		total := big.NewInt(0)
		for _, a := range accs {
			codes = append(codes, a.Code)
			if a.EntriesBalance != nil {
				total.Add(total, a.EntriesBalance)
			}
		}
		sort.Strings(codes)
		c.Findings = append(c.Findings, fmt.Sprintf(
			"%s:%s in scope %q is addressed by %d accounts (%v); their balances sum to %s, "+
				"which every magnitude comparison would accept",
			k.key.Chain, k.key.Contract, k.scope, len(accs), codes, total.String()))
	}

	return c
}

// accountScope names the position an account addresses, apart from the asset:
// the wallet itself, or one protocol the wallet has a position with.
//
// It is derived from the account CODE rather than from the type, because the
// protocol qualifier lives only in the code — `collateral.{proto}.{walletID}.…`
// — and two protocols holding the same asset are two legitimate accounts of the
// same type. Reading the type alone would merge them and report correct
// bookkeeping as a split.
func accountScope(a LedgerAccount) string {
	parts := strings.Split(a.Code, ".")
	switch {
	case len(parts) >= 2 && (parts[0] == "collateral" || parts[0] == "liability"):
		// collateral.{proto}.{walletID}.{chain}.{asset} — the protocol is the
		// scope. An empty proto (the code shape allows it) still scopes
		// consistently, so a genuine split inside one protocol is still caught.
		return parts[0] + "." + parts[1]
	case len(parts) >= 1:
		return parts[0]
	default:
		return a.Type
	}
}

// triangleInput is everything the two P-dependent checks need, gathered so the
// caller can state plainly whether P is available.
type triangleInput struct {
	positions []Position
	// available is false when the provider could not be reached. The F↔L half of
	// the triangle still runs; the P column and the verdict do not.
	available bool
	// unavailableReason explains why, and appears verbatim in the report.
	unavailableReason string
}

// buildRows assembles the P∪L∪F union into attributed rows and returns the two
// P-dependent checks alongside them.
//
// The three quantities are gathered per (chain, contract):
//
//	P — the provider's position, normalized by THIS package
//	F — the net flow of the collected history, from sync.NetFlows (#60)
//	L — SUM(debit) − SUM(credit) over the ledger's entries
//
// F↔L is the DIAGNOSIS: both sides come from the same collected history, so a
// difference between them can only be a posting error, and when it is non-zero
// the defect is located without consulting P at all. P↔L is the VERDICT.
func buildRows(
	ledger *LedgerSnapshot,
	flows []sync.AssetNetFlow,
	knownness map[sync.AssetKey]string,
	tri triangleInput,
) ([]Row, Check, Check) {
	triangle := Check{Name: CheckNameTriangle, Status: CheckRan, Findings: []string{}}
	verdict := Check{Name: CheckNameVerdict, Status: CheckRan, Findings: []string{}}

	// The triangle keeps RUNNING when the provider is down, and this is the one
	// place that deserves spelling out. Its P column is unavailable, but its F↔L
	// edge — the DIAGNOSIS — is built entirely from the database and needs no
	// network at all. Marking the whole triangle not-run would throw away the
	// more trustworthy of its two edges because the other one is missing, which
	// is the same mistake as failing the whole command: the ticket puts checks
	// 1–3 in the networkless group precisely so this cannot happen.
	//
	// Only the VERDICT is genuinely unreachable without P.
	if !tri.available {
		triangle.PartialReason = tri.unavailableReason
		verdict.Status = CheckNotRun
		verdict.NotRunReason = tri.unavailableReason
	}

	ledgerBalances := ledger.LedgerBalances()
	ledgerSymbols := ledger.Symbols()

	positionByKey := make(map[sync.AssetKey]Position, len(tri.positions))
	if tri.available {
		for _, p := range tri.positions {
			// Two provider rows on one identity are summed rather than one
			// silently winning: dropping a duplicate would understate P and turn
			// a provider quirk into a ledger finding.
			if prev, ok := positionByKey[p.Key]; ok {
				prev.Quantity = new(big.Int).Add(prev.Quantity, p.Quantity)
				positionByKey[p.Key] = prev
				continue
			}
			positionByKey[p.Key] = p
		}
	}

	flowByKey := make(map[sync.AssetKey]sync.AssetNetFlow, len(flows))
	for _, f := range flows {
		flowByKey[f.Key] = f
	}

	// The union of all three sides. An identity present in only one of them is
	// exactly the interesting case, so none of the three may be treated as the
	// index.
	keySet := make(map[sync.AssetKey]bool)
	for k := range positionByKey {
		keySet[k] = true
	}
	for k := range ledgerBalances {
		keySet[k] = true
	}
	for k := range flowByKey {
		keySet[k] = true
	}
	keys := make([]sync.AssetKey, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sortKeys(keys)

	rows := make([]Row, 0, len(keys))
	for _, k := range keys {
		pos, hasP := positionByKey[k]
		flow, hasF := flowByKey[k]
		lb, hasL := ledgerBalances[k]

		row := Row{
			Chain:           k.Chain,
			Contract:        k.Contract,
			KnownnessStatus: knownness[k],
		}

		// Display metadata, taken from whichever side has it. The ledger's
		// registry symbol is preferred because it is the name the rest of the
		// application shows; the provider's is the fallback for an identity the
		// ledger has never seen.
		switch {
		case ledgerSymbols[k] != "":
			row.Symbol = ledgerSymbols[k]
		case hasP:
			row.Symbol = pos.Symbol
		case hasF:
			row.Symbol = flow.AssetSymbol
		}
		switch {
		case hasP:
			row.Decimals = pos.Decimals
		case hasF:
			row.Decimals = flow.Decimals
		}

		if hasP {
			row.Provider = pos.Quantity.String()
		}
		if hasF {
			row.Flow = flow.NetFlow.String()
			for _, r := range flow.RejectedBy {
				row.RejectedBy = append(row.RejectedBy, string(r))
			}
			if flow.RejectedAmount != nil && flow.RejectedAmount.Sign() != 0 {
				row.RejectedAmount = flow.RejectedAmount.String()
			}
		}
		if hasL {
			row.Ledger = lb.String()
		}

		// EDGE F↔L — the diagnosis. Computed whether or not P is available,
		// because it does not depend on P and is the more trustworthy of the two
		// edges: a difference here can ONLY be a posting error.
		if hasF && hasL {
			fl := new(big.Int).Sub(flow.NetFlow, lb)
			if fl.Sign() != 0 {
				row.FlowLedgerDelta = fl.String()
				if abs(fl).Cmp(positionTolerance) > 0 {
					triangle.Findings = append(triangle.Findings, fmt.Sprintf(
						"%s:%s (%s): flow=%s ledger=%s delta=%s — both sides come from the "+
							"same collected history, so this is a POSTING defect",
						k.Chain, k.Contract, row.Symbol, flow.NetFlow.String(), lb.String(), fl.String()))
				}
			}
		}

		row.Category, row.Delta = categorize(sides{
			key:         k,
			provider:    pos.Quantity,
			hasProvider: hasP,
			flow:        flow,
			hasFlow:     hasF,
			ledger:      lb,
			hasLedger:   hasL,
			knownness:   knownness[k],
		})

		if row.Category.IsRed() && tri.available {
			verdict.Findings = append(verdict.Findings, redFinding(row))
		}

		// An identity both sides call empty is not printed. Its absence from the
		// rows is the report's answer, not an omission.
		//
		// The exception is an asset a RULE kept out. #41 requires the filtered
		// assets listed by name with quantities, and dropping the ones the
		// provider has since stopped reporting would silently shorten exactly
		// that list — the reader would have no way to tell "nothing was filtered"
		// from "what was filtered is no longer visible".
		if row.Category == CategorySettled && row.RejectedAmount == "" {
			continue
		}

		rows = append(rows, row)
	}

	return rows, triangle, verdict
}

// sides is what the three inputs say about one asset identity.
//
// The presence flags travel WITH their values because "absent" and "present and
// zero" are different facts throughout this report — a missing ledger account
// and an account holding nothing are not the same state — and a bare *big.Int
// cannot express the difference. Bundling them keeps the pair from being split
// across a long parameter list where one half can be read without the other.
type sides struct {
	key sync.AssetKey

	provider    *big.Int
	hasProvider bool

	flow    sync.AssetNetFlow
	hasFlow bool

	ledger    *big.Int
	hasLedger bool

	knownness string
}

// ledgerAmount is L as a number, treating an absent account as zero. Callers
// that need to tell absent from zero read hasLedger instead.
func (s sides) ledgerAmount() *big.Int {
	if !s.hasLedger || s.ledger == nil {
		return big.NewInt(0)
	}
	return s.ledger
}

// categorize assigns the attribution for one identity, returning the category
// and the P − L delta (empty when one side has no value to subtract).
//
// The order of the branches is the decision. "In P, not in L" is checked for an
// EXPLANATION before it is called a finding, because after genesis was switched
// off (#49) that category fills up as normal operation — a receipt rule and a
// knownness filter both remove from L things the provider keeps reporting in P.
// Only what no rule accounts for is red.
func categorize(s sides) (Category, string) {
	if s.hasProvider {
		l := s.ledgerAmount()

		// An identity the ledger holds a balance in is COMPARED, never explained.
		// A rule excuses a missing balance only when there is no balance to miss;
		// letting an explanation outrank a real ledger amount would let one
		// rejected leg make an asset permanently unflaggable, which is the silent
		// papering-over that switching genesis off (#49) was meant to end.
		//
		// The test is EXISTENCE of a ledger account, not the size of its balance.
		// An account posted down to exactly zero is not the same fact as an asset
		// the ledger never held: the first says "we tracked this and it is spent",
		// which the provider is contradicting, while the second says "we never
		// booked it", which a rule can explain. Testing the amount instead would
		// file a real over-spend — the one case where the ledger is demonstrably
		// wrong — under whatever explanation happened to be available.
		if s.hasLedger {
			delta := new(big.Int).Sub(s.provider, l)
			out := ""
			if delta.Sign() != 0 {
				out = delta.String()
			}
			if abs(delta).Cmp(positionTolerance) > 0 {
				return CategoryAmountMismatch, out
			}
			return CategoryAgrees, out
		}

		// Present in P, and the ledger holds no account for it at all. Find out
		// why. The exemption is exact: a rule excuses a missing balance only when
		// there is no balance to miss, which is what reaching this branch means.
		if s.hasFlow && s.flow.Explained() {
			if hasRejection(s.flow, sync.RejectionReceipt) {
				return CategoryExplainedReceipt, ""
			}
			if hasRejection(s.flow, sync.RejectionUnknownAsset) {
				return knownnessCategory(s.knownness), ""
			}
		}

		// No flow entry AND no rejection: the collected history never mentioned
		// this asset at all.
		return knownnessCategory(s.knownness), ""
	}

	// Held in the ledger, not reported by the provider. Printed rather than
	// reddened: a DeFi wrapper the balances endpoint cannot see looks exactly like
	// this, and so does an asset spent down to a residue. It is not silently green
	// either — it is a named row a human reads once and recognises.
	if s.ledgerAmount().Sign() != 0 {
		return CategoryLedgerOnly, ""
	}

	// Absent from P and holding nothing in the ledger. Both sides say "no
	// position", so the caller drops it rather than printing it: every asset the
	// wallet has ever fully spent would otherwise drown the report.
	return CategorySettled, ""
}

// knownnessCategory turns a stored knownness verdict into the attribution for a
// position the ledger does not hold.
//
// The three states stay three. "Checked, and the answer is no" is spam handled
// correctly; "queued, not yet checked" is a queue that may be hiding a migration
// bug; "never enqueued" means no leg of this asset was ever seen, so nothing in
// the pipeline can account for the position at all. #58 went to some trouble to
// keep the first two apart, and only the third is a finding.
func knownnessCategory(status string) Category {
	switch status {
	case string(sync.KnownnessUnknown):
		return CategoryExplainedUnknownAsset
	case string(sync.KnownnessPending):
		return CategoryCheckFailed
	default:
		return CategoryUnexplainedMissingFromLedger
	}
}

func hasRejection(f sync.AssetNetFlow, want sync.RejectionReason) bool {
	for _, r := range f.RejectedBy {
		if r == want {
			return true
		}
	}
	return false
}

func redFinding(row Row) string {
	switch row.Category {
	case CategoryAmountMismatch:
		return fmt.Sprintf("%s:%s (%s): provider=%s ledger=%s delta=%s",
			row.Chain, row.Contract, row.Symbol, row.Provider, row.Ledger, row.Delta)
	default:
		return fmt.Sprintf("%s:%s (%s): provider reports %s, ledger holds nothing, "+
			"and no rule accounts for the absence (knownness=%q)",
			row.Chain, row.Contract, row.Symbol, row.Provider, row.KnownnessStatus)
	}
}

func abs(x *big.Int) *big.Int { return new(big.Int).Abs(x) }

func sortKeys(keys []sync.AssetKey) {
	sort.Slice(keys, func(i, j int) bool {
		ci, xi := keyOf(keys[i])
		cj, xj := keyOf(keys[j])
		if ci != cj {
			return ci < cj
		}
		return xi < xj
	})
}
