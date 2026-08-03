package reconcilereport

import (
	"bytes"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/sync/assetlist"
)

// The tests run on the SAME snapshot the operator runs on — it is the fixture
// and the second entrance of the command at once, which is what keeps the tested
// path and the operated path from drifting apart.
const snapshotPath = "testdata/snapshot.json"

var testWalletID = uuid.MustParse("19934671-b0c2-434d-8f33-e27dae4db78b")

func bi(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad big int: " + s)
	}
	return n
}

func account(code, chain, contract, symbol, entries string, materialized *big.Int) LedgerAccount {
	return LedgerAccount{
		AccountID:           uuid.New(),
		Code:                code,
		Type:                AccountTypeCryptoWallet,
		Key:                 sync.NewAssetKey(chain, contract),
		Symbol:              symbol,
		EntriesBalance:      bi(entries),
		MaterializedBalance: materialized,
	}
}

// collateralAccount builds an account in a protocol's namespace: the same asset
// the wallet also holds, supplied to a lending market. Two accounts, one
// identity, and legitimately so.
func collateralAccount(proto, chain, contract, symbol, entries string) LedgerAccount {
	a := account("collateral."+proto+".w."+chain+"."+symbol, chain, contract, symbol, entries, bi(entries))
	a.Type = "COLLATERAL"
	return a
}

// flowOf builds an F row for an asset whose legs DID reach the ledger.
func flowOf(chain, contract, symbol string, decimals int, net string) sync.AssetNetFlow {
	return sync.AssetNetFlow{
		Key:            sync.NewAssetKey(chain, contract),
		AssetSymbol:    symbol,
		Decimals:       decimals,
		NetFlow:        bi(net),
		Booked:         true,
		RejectedAmount: big.NewInt(0),
	}
}

// rejectedFlow builds an F row for an asset whose EVERY leg was rejected — the
// shape sync.NetFlows emits for a receipt or a filtered token, and the one that
// makes Explained() true.
func rejectedFlow(chain, contract, symbol string, decimals int, reason sync.RejectionReason, amount string) sync.AssetNetFlow {
	return sync.AssetNetFlow{
		Key:            sync.NewAssetKey(chain, contract),
		AssetSymbol:    symbol,
		Decimals:       decimals,
		NetFlow:        big.NewInt(0),
		RejectedBy:     []sync.RejectionReason{reason},
		Booked:         false,
		RejectedAmount: bi(amount),
	}
}

// baselineInput is the state the snapshot describes when everything is as it
// should be: real USDC and native ETH booked and agreeing, an Aave receipt kept
// out by the receipt rule, and a homoglyph spam token kept out by the knownness
// filter with a verdict recorded.
func baselineInput(t *testing.T) Input {
	t.Helper()

	snap, err := LoadSnapshot(snapshotPath)
	require.NoError(t, err)
	positions, err := snap.Positions()
	require.NoError(t, err)

	ledger := &LedgerSnapshot{
		WalletID: testWalletID,
		Accounts: []LedgerAccount{
			account("wallet.w.base.usdc", "base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
				"USDC", "710621363", bi("710621363")),
			account("wallet.w.base.native", "base", sync.NativeContract,
				"ETH", "1500000000000000000", bi("1500000000000000000")),
		},
		ChainCursors: map[string]string{"base": "2026-07-21T21:05:07Z"},
	}

	return Input{
		WalletID:      testWalletID.String(),
		WalletAddress: snap.WalletAddress,
		Ledger:        ledger,
		Flows: []sync.AssetNetFlow{
			flowOf("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", "USDC", 6, "710621363"),
			flowOf("base", sync.NativeContract, "ETH", 18, "1500000000000000000"),
			rejectedFlow("base", "0xbdb9300b7cde636d9cd4aff00f6f009ffbbc8ee6", "aBascbBTC", 8,
				sync.RejectionReceipt, "11219239"),
			rejectedFlow("base", "0xeb9caafc9cd52434fc906dc6ef28f24509d9b309", "UЅDС", 18,
				sync.RejectionUnknownAsset, "7090500000000000000000"),
		},
		Knownness: map[sync.AssetKey]string{
			sync.NewAssetKey("base", "0xeb9caafc9cd52434fc906dc6ef28f24509d9b309"): string(sync.KnownnessUnknown),
		},
		Positions:          positions,
		PositionsAvailable: true,
		PositionsSource:    "snapshot " + snapshotPath,
		PositionsFetchedAt: snap.CapturedAt,
		CursorProbed:       snap.CursorProbed,
	}
}

// MANDATORY TEST 1 — the regression on the amendment from #49.
//
// Decision #41 originally reddened the whole "in P, not in L" category. After
// genesis was switched off, that category fills up as normal operation: a
// receipt rule and a knownness filter both remove from the ledger things the
// provider keeps reporting. A report that reddened on it would be red on every
// wallet with DeFi or spam and would stop meaning anything.
//
// So: every difference here is attributed, and the verdict must be GREEN.
func TestReport_GreenWhenEveryDifferenceIsExplained(t *testing.T) {
	r := Build(baselineInput(t))

	assert.Empty(t, r.RedRows(), "no row may be red when every difference is attributed")
	assert.Equal(t, 0, r.Summary.Red)
	assert.Equal(t, 0, r.ExitCode(), "the red category is empty, so the verdict is 0")

	byCat := map[Category]Row{}
	for _, row := range r.Rows {
		byCat[row.Category] = row
	}

	receipt, ok := byCat[CategoryExplainedReceipt]
	require.True(t, ok, "the Aave receipt must be present and attributed to the receipt rule")
	assert.Equal(t, "0xbdb9300b7cde636d9cd4aff00f6f009ffbbc8ee6", receipt.Contract)
	assert.Equal(t, "11219239", receipt.Provider,
		"the receipt is OBLIGED to show up in P: filtering the P side by the receipt rule "+
			"would make both sides agree by construction")
	assert.Equal(t, "", receipt.Ledger, "and equally obliged to be absent from L")

	spam, ok := byCat[CategoryExplainedUnknownAsset]
	require.True(t, ok, "the filtered token must be present, not merely counted")
	assert.Equal(t, "0xeb9caafc9cd52434fc906dc6ef28f24509d9b309", spam.Contract,
		"filtered assets are listed by name: a count cannot tell spam from a broken resolve")
	assert.Equal(t, "UЅDС", spam.Symbol)
	assert.Equal(t, "7090500000000000000000", spam.Provider)
	assert.Equal(t, "7090500000000000000000", spam.RejectedAmount,
		"contract + symbol + QUANTITY: a name alone cannot separate a dust airdrop "+
			"from a real holding the filter wrongly rejected")
	assert.Equal(t, []string{string(sync.RejectionUnknownAsset)}, spam.RejectedBy)
	assert.Equal(t, "11219239", receipt.RejectedAmount,
		"the receipt's size is reported too, not only its name")

	for _, c := range r.Checks {
		assert.Equal(t, CheckRan, c.Status, "check %s must run", c.Name)
		assert.Empty(t, c.Findings, "check %s must find nothing", c.Name)
	}
}

// MANDATORY TEST 2 — a position no category accounts for is RED.
//
// The counterpart of test 1: the amendment narrowed the red category, it did not
// remove it. A provider position with no ledger balance, no rejection and no
// knownness verdict is exactly what "not explained by anything" means.
func TestReport_RedOnPositionNoCategoryExplains(t *testing.T) {
	in := baselineInput(t)

	// A position the collected history never mentioned, that no rule rejected,
	// and that the knownness registry has never even been asked about.
	orphan := sync.NewAssetKey("base", "0x1111111111111111111111111111111111111111")
	in.Positions = append(in.Positions, Position{
		Key:      orphan,
		Symbol:   "ORPHAN",
		Decimals: 18,
		Quantity: bi("4000000000000000000"),
	})

	r := Build(in)

	red := r.RedRows()
	require.Len(t, red, 1, "exactly the unattributed position must be red")
	assert.Equal(t, CategoryUnexplainedMissingFromLedger, red[0].Category)
	assert.Equal(t, orphan.Contract, red[0].Contract)
	assert.Equal(t, 1, r.ExitCode(), "a red row means exit 1")

	// Exit 1 must not truncate the report: the explained rows are still there.
	assert.Greater(t, len(r.Rows), 1, "the report is printed IN FULL at exit 1")
	assert.Contains(t, categorySet(r), CategoryExplainedReceipt,
		"the green rows survive alongside the red one")

	var buf bytes.Buffer
	require.NoError(t, WriteJSON(&buf, r))
	assert.Contains(t, buf.String(), "0x1111111111111111111111111111111111111111")
	assert.Contains(t, buf.String(), "0xbdb9300b7cde636d9cd4aff00f6f009ffbbc8ee6",
		"the full report is emitted at exit 1, not just the findings")
}

// MANDATORY TEST 3 — the split account.
//
// This is the check that cannot be replaced by any comparison of magnitudes.
// The two halves here sum to EXACTLY the right total, so the triangle is
// perfectly happy — including its F↔L diagnosis edge, whose two sides come from
// one source. Only counting the accounts finds it.
func TestReport_AccountShapeCatchesSplitWithCorrectTotals(t *testing.T) {
	in := baselineInput(t)

	// Split the USDC account in two. 710621363 = 400000000 + 310621363.
	in.Ledger.Accounts = []LedgerAccount{
		account("wallet.w.base.usdc.a", "base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
			"USDC", "400000000", bi("400000000")),
		account("wallet.w.base.usdc.b", "base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
			"USDC", "310621363", bi("310621363")),
		account("wallet.w.base.native", "base", sync.NativeContract,
			"ETH", "1500000000000000000", bi("1500000000000000000")),
	}

	r := Build(in)

	shape := checkByName(t, r, CheckNameAccountShape)
	require.Len(t, shape.Findings, 1, "the split identity must be reported")
	assert.Contains(t, shape.Findings[0], "addressed by 2 accounts")
	assert.Contains(t, shape.Findings[0], "710621363",
		"the finding states the correct total, which is what makes it invisible elsewhere")

	// The point of the test: every magnitude comparison accepts the split.
	assert.Empty(t, r.RedRows(),
		"aggregation by identity makes two jointly-correct accounts indistinguishable "+
			"from one correct account, so no amount comparison can see this")
	assert.Empty(t, checkByName(t, r, CheckNameTriangle).Findings,
		"F↔L is clean too: the split is invisible to the diagnosis edge")
	assert.Empty(t, checkByName(t, r, CheckNameMaterialization).Findings,
		"and both halves are correctly materialized")

	assert.Equal(t, 1, r.ExitCode(), "the shape finding alone must fail the report")
}

// TestReport_CollateralIsCheckedButDoesNotPolluteTheTriangle covers the
// namespace boundary that #49's own evidence sat on: the asset it caught was in
// `collateral..{walletID}.base.variableDebtBasUSDC`, so a check that skipped
// that namespace would be blind to the case that motivated it.
//
// Both halves are asserted, because each is wrong without the other. Checks 2
// and 3 must SEE collateral; the triangle must NOT sum it into L, since the
// provider's balancesOf reports only what the wallet address holds and an asset
// supplied to a market has left it.
func TestReport_CollateralIsCheckedButDoesNotPolluteTheTriangle(t *testing.T) {
	usdc := "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"

	t.Run("supplying an asset the wallet also holds is not a split", func(t *testing.T) {
		in := baselineInput(t)
		in.Ledger.Accounts = append(in.Ledger.Accounts,
			collateralAccount("aave", "base", usdc, "USDC", "5000000"))

		r := Build(in)
		assert.Empty(t, checkByName(t, r, CheckNameAccountShape).Findings,
			"collateral is protocol-scoped by design: the same asset held and supplied "+
				"is two accounts in two scopes, not one identity split in two")
		assert.Empty(t, r.RedRows(),
			"and the supplied amount must not be added to L: the provider cannot see it, "+
				"so counting it would make every supplied position look like a missing balance")

		for _, row := range r.Rows {
			if row.Contract == usdc {
				assert.Equal(t, "710621363", row.Ledger,
					"L is the wallet namespace alone")
			}
		}
	})

	t.Run("a split INSIDE one protocol is still caught", func(t *testing.T) {
		in := baselineInput(t)
		in.Ledger.Accounts = append(in.Ledger.Accounts,
			collateralAccount("aave", "base", usdc, "USDC", "2000000"),
			LedgerAccount{
				AccountID:           uuid.New(),
				Code:                "collateral.aave.w.base.USDC-dup",
				Type:                "COLLATERAL",
				Key:                 sync.NewAssetKey("base", usdc),
				Symbol:              "USDC",
				EntriesBalance:      bi("3000000"),
				MaterializedBalance: bi("3000000"),
			})

		shape := checkByName(t, Build(in), CheckNameAccountShape)
		require.Len(t, shape.Findings, 1,
			"two accounts in ONE protocol scope is the split this check exists for")
		assert.Contains(t, shape.Findings[0], "collateral.aave")
		assert.Contains(t, shape.Findings[0], "5000000", "the totals are correct, which is the point")
	})

	t.Run("a stale materialization on a collateral account is caught", func(t *testing.T) {
		in := baselineInput(t)
		stale := collateralAccount("aave", "base", usdc, "USDC", "5000000")
		stale.MaterializedBalance = bi("1")
		in.Ledger.Accounts = append(in.Ledger.Accounts, stale)

		mat := checkByName(t, Build(in), CheckNameMaterialization)
		require.Len(t, mat.Findings, 1,
			"check 2 covers every account the wallet owns, collateral included — "+
				"#49's evidence was exactly such an account")
		assert.Contains(t, mat.Findings[0], "collateral.aave")
	})
}

// MANDATORY TEST 4 — stale materialization.
//
// It must show up in check 2 AND must NOT produce a false diagnosis in the
// triangle. That is the whole reason L is taken from the entries: reading L out
// of account_balances would make a stale cache look like a posting error, and
// the diagnosis would name the wrong defect.
func TestReport_StaleMaterializationIsCaughtWithoutFalseDiagnosis(t *testing.T) {
	in := baselineInput(t)

	// The entries are right; the materialized row is stale by a wide margin.
	in.Ledger.Accounts = []LedgerAccount{
		account("wallet.w.base.usdc", "base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
			"USDC", "710621363", bi("123456789")),
		account("wallet.w.base.native", "base", sync.NativeContract,
			"ETH", "1500000000000000000", bi("1500000000000000000")),
	}

	r := Build(in)

	mat := checkByName(t, r, CheckNameMaterialization)
	require.Len(t, mat.Findings, 1, "the drift must be reported by check 2")
	assert.Contains(t, mat.Findings[0], "entries=710621363")
	assert.Contains(t, mat.Findings[0], "materialized=123456789")

	// The triangle must stay silent: nothing about the POSTING is wrong.
	assert.Empty(t, checkByName(t, r, CheckNameTriangle).Findings,
		"a stale cache is not a posting error; L comes from the entries precisely "+
			"so the diagnosis does not name the wrong defect")
	assert.Empty(t, r.RedRows(),
		"and the verdict edge is unaffected: P still agrees with the entries")

	for _, row := range r.Rows {
		if row.Contract == "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913" {
			assert.Equal(t, "710621363", row.Ledger,
				"L is SUM(debit)-SUM(credit), never the materialized value")
			assert.Equal(t, CategoryAgrees, row.Category)
		}
	}

	assert.Equal(t, 1, r.ExitCode())
}

// MANDATORY TEST 5 — the provider is unreachable.
//
// The command must not fail as a whole. Three checks run; the two that need P
// are marked not-run with a reason; the exit code is 2, not 1 — "was not
// checked" is a different answer from "did not add up".
func TestReport_ProviderUnavailableRunsTheNetworklessChecks(t *testing.T) {
	in := baselineInput(t)
	in.Positions = nil
	in.PositionsAvailable = false
	in.PositionsSource = "provider (unavailable)"
	in.PositionsUnavailableReason = "dial tcp: connection refused"
	in.PositionsFetchedAt = time.Time{}

	r := Build(in)

	assert.Equal(t, CheckRan, checkByName(t, r, CheckNameMaterialization).Status)
	assert.Equal(t, CheckRan, checkByName(t, r, CheckNameAccountShape).Status)

	// The triangle keeps running: its F↔L edge is built from the database alone.
	// Only its P column is missing, which is stated as a PARTIAL rather than
	// throwing away a working diagnosis because an unrelated input is absent.
	tri := checkByName(t, r, CheckNameTriangle)
	assert.Equal(t, CheckRan, tri.Status,
		"the F↔L diagnosis needs no network and must survive an unreachable provider")
	assert.Contains(t, tri.PartialReason, "connection refused",
		"but the report must say which part of it could not be performed")

	verdict := checkByName(t, r, CheckNameVerdict)
	assert.Equal(t, CheckNotRun, verdict.Status,
		"the verdict is the one thing genuinely unreachable without P")
	assert.Contains(t, verdict.NotRunReason, "connection refused",
		"a check marked not-run must say why")

	assert.Equal(t, 2, r.ExitCode(),
		"a check that could not run means no verdict, which is 2 and never 1")
	assert.Equal(t, 1, r.Summary.ChecksNotRun,
		"exactly one check is unreachable; the other three ran")

	// The F↔L rows are still produced: losing the reliable checks because the
	// unreliable one is down is exactly what this split prevents.
	assert.NotEmpty(t, r.Rows)
	for _, row := range r.Rows {
		assert.Empty(t, row.Provider, "no P column is available")
	}
}

// TestChecksNeedNoNetwork pins «проверки 1–3 сети не требуют» as a property of
// the code rather than a claim in a comment.
//
// It is enforced structurally, which is the only way that holds: Build performs
// no I/O at all — it takes values, not handles — so no check it runs CAN reach
// the network. The two P-dependent parts get their input handed to them already
// fetched, and the test demonstrates the three networkless checks producing
// findings from a ledger snapshot alone, with the provider absent entirely.
func TestChecksNeedNoNetwork(t *testing.T) {
	in := baselineInput(t)
	in.Positions = nil
	in.PositionsAvailable = false
	in.PositionsUnavailableReason = "no network in this test"

	// Give each networkless check something to find, so a passing result cannot
	// come from there being nothing to check.
	in.Ledger.Accounts = []LedgerAccount{
		// Split across two accounts (check 3), stale on one of them (check 2), and
		// summing to less than the collected history says was booked (check 1's
		// F↔L edge). Three different defects, none of which needs a provider.
		account("wallet.w.base.usdc.a", "base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
			"USDC", "400000000", bi("400000000")),
		account("wallet.w.base.usdc.b", "base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
			"USDC", "100000000", bi("1")),
	}

	r := Build(in)

	assert.NotEmpty(t, checkByName(t, r, CheckNameTriangle).Findings,
		"check 1's F↔L edge is built from the database alone")
	assert.NotEmpty(t, checkByName(t, r, CheckNameMaterialization).Findings,
		"check 2 reads two columns of the ledger and nothing else")
	assert.NotEmpty(t, checkByName(t, r, CheckNameAccountShape).Findings,
		"check 3 counts accounts and nothing else")

	assert.Equal(t, CheckNotRun, checkByName(t, r, CheckNameVerdict).Status,
		"only the verdict genuinely needs P")
}

// TestReport_DeterministicAcrossRuns proves the property acceptance depends on:
// two runs over one snapshot produce byte-identical JSON. Without it, diffing
// two runs — the main way this report is used — is impossible.
func TestReport_DeterministicAcrossRuns(t *testing.T) {
	var first, second bytes.Buffer

	r1 := Build(baselineInput(t))
	require.NoError(t, WriteJSON(&first, r1))

	r2 := Build(baselineInput(t))
	require.NoError(t, WriteJSON(&second, r2))

	assert.Equal(t, first.String(), second.String(),
		"two runs on one snapshot must be byte-identical")
	assert.NotContains(t, first.String(), time.Now().Format("2006-01-02T15:04"),
		"no value in the report may be the moment it ran")
}

// TestReport_AmountMismatchIsRed covers the second red category: both sides hold
// the asset and the quantities disagree beyond the per-position allowance.
func TestReport_AmountMismatchIsRed(t *testing.T) {
	in := baselineInput(t)
	in.Ledger.Accounts[0].EntriesBalance = bi("600000000") // provider says 710621363
	in.Ledger.Accounts[0].MaterializedBalance = bi("600000000")
	in.Flows[0] = flowOf("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", "USDC", 6, "600000000")

	r := Build(in)

	red := r.RedRows()
	require.Len(t, red, 1)
	assert.Equal(t, CategoryAmountMismatch, red[0].Category)
	assert.Equal(t, "110621363", red[0].Delta, "P − L, in base units")
	assert.Equal(t, 1, r.ExitCode())
}

// TestReport_LedgerPostedToZeroWhileProviderHoldsIsRed closes the gap where an
// over-spend hid behind an explanation.
//
// An account posted down to exactly zero is not the same as an asset the ledger
// never held. If a rule's rejection could absorb it, the one case where the
// LEDGER is demonstrably wrong — it says nothing is left, the chain says
// otherwise — would be filed as green.
func TestReport_LedgerPostedToZeroWhileProviderHoldsIsRed(t *testing.T) {
	in := baselineInput(t)

	// The wallet's USDC account exists and is posted to exactly zero, while the
	// provider still reports 710.621363 USDC.
	in.Ledger.Accounts[0].EntriesBalance = bi("0")
	in.Ledger.Accounts[0].MaterializedBalance = bi("0")
	// And a rejection exists for the same asset, so an explanation is available
	// to be wrongly applied.
	in.Flows[0] = rejectedFlow("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
		"USDC", 6, sync.RejectionUnknownAsset, "5")
	in.Knownness[sync.NewAssetKey("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913")] =
		string(sync.KnownnessUnknown)

	r := Build(in)

	var row Row
	for _, c := range r.Rows {
		if c.Contract == "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913" {
			row = c
		}
	}
	assert.Equal(t, CategoryAmountMismatch, row.Category,
		"a rejection cannot excuse a position when the ledger holds an account for it: "+
			"the exemption applies only when there is no balance to miss, and here the "+
			"two sides state different quantities for an asset both of them track")
	assert.Equal(t, "710621363", row.Delta, "P − L, with L at zero")
	assert.True(t, row.Category.IsRed())
	assert.Equal(t, 1, r.ExitCode())
}

// TestReport_ToleranceIsInBaseUnitsNotPercent pins the choice of allowance.
// Attribution replaced the threshold; what remains is a handful of base units
// against honest rebase drift, and it must not scale with the position.
func TestReport_ToleranceIsInBaseUnitsNotPercent(t *testing.T) {
	t.Run("a few base units off a large position is tolerated", func(t *testing.T) {
		in := baselineInput(t)
		in.Ledger.Accounts[0].EntriesBalance = bi("710621358") // 5 base units short
		in.Ledger.Accounts[0].MaterializedBalance = bi("710621358")
		in.Flows[0] = flowOf("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", "USDC", 6, "710621358")

		assert.Empty(t, Build(in).RedRows())
	})

	t.Run("a dropped leg of a cheap token is caught although a percentage would pass it", func(t *testing.T) {
		in := baselineInput(t)
		// 0.0001% of the position — any percentage threshold accepts this.
		in.Ledger.Accounts[0].EntriesBalance = bi("710620363")
		in.Ledger.Accounts[0].MaterializedBalance = bi("710620363")
		in.Flows[0] = flowOf("base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", "USDC", 6, "710620363")

		red := Build(in).RedRows()
		require.Len(t, red, 1)
		assert.Equal(t, CategoryAmountMismatch, red[0].Category)
	})
}

// TestReport_FlowLedgerEdgeDiagnosesPosting proves the diagnosis edge does its
// job: F and L come from the same collected history, so a difference between
// them can only be a posting defect and is reported as one, without consulting P.
func TestReport_FlowLedgerEdgeDiagnosesPosting(t *testing.T) {
	in := baselineInput(t)
	// The history says 710621363 was booked; the ledger holds less. Same source,
	// two answers.
	in.Ledger.Accounts[0].EntriesBalance = bi("500000000")
	in.Ledger.Accounts[0].MaterializedBalance = bi("500000000")

	r := Build(in)

	tri := checkByName(t, r, CheckNameTriangle)
	require.Len(t, tri.Findings, 1)
	assert.Contains(t, tri.Findings[0], "POSTING defect")
	assert.Contains(t, tri.Findings[0], "flow=710621363")
	assert.Contains(t, tri.Findings[0], "ledger=500000000")

	for _, row := range r.Rows {
		if row.Contract == "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913" {
			assert.Equal(t, "210621363", row.FlowLedgerDelta)
		}
	}
}

// TestReport_FilteredAssetSurvivesEvenWhenTheProviderStopsReportingIt keeps the
// by-name list complete. An asset the filter rejected and the provider no longer
// shows would otherwise be dropped as "settled", silently shortening exactly the
// list #41 asked for — and a reader could not tell "nothing was filtered" from
// "what was filtered is no longer visible".
func TestReport_FilteredAssetSurvivesEvenWhenTheProviderStopsReportingIt(t *testing.T) {
	in := baselineInput(t)

	// A token the filter rejected, absent from both P and L today.
	gone := sync.NewAssetKey("base", "0x2222222222222222222222222222222222222222")
	in.Flows = append(in.Flows, rejectedFlow("base", gone.Contract, "GONE", 18,
		sync.RejectionUnknownAsset, "500000000000000000"))
	in.Knownness[gone] = string(sync.KnownnessUnknown)

	r := Build(in)

	var found bool
	for _, row := range r.Rows {
		if row.Contract == gone.Contract {
			found = true
			assert.Equal(t, "GONE", row.Symbol)
			assert.Equal(t, "500000000000000000", row.RejectedAmount,
				"its size is what tells spam from a broken resolve")
			assert.Equal(t, "", row.Provider, "the provider no longer reports it")
		}
	}
	assert.True(t, found, "a filtered asset must stay in the by-name list")
	assert.Empty(t, r.RedRows(), "and it is still explained, so still green")
}

// TestReport_UnpostedRawsAreNamedInTheDiagnosis pins the attribution of the
// commonest cause of an F↔L gap. F counts every collected transaction while L
// holds only those that posted, so a raw that errored is a real hole — but one
// whose cause is already written on the raw, and a diagnosis that reports the
// symptom alone sends the reader hunting for a posting bug that is recorded.
func TestReport_UnpostedRawsAreNamedInTheDiagnosis(t *testing.T) {
	in := baselineInput(t)
	in.UnpostedRaws = map[string]int{"error": 25, "skipped": 192, "processed": 164}

	tri := checkByName(t, Build(in), CheckNameTriangle)
	require.NotEmpty(t, tri.Findings)
	last := tri.Findings[len(tri.Findings)-1]
	assert.Contains(t, last, "217 collected transaction(s) never reached the ledger",
		"processed raws are not counted; skipped and errored both leave the same hole")
	assert.Contains(t, last, "error=25")
	assert.Contains(t, last, "skipped=192")
	assert.NotContains(t, last, "processed=",
		"a posted transaction is not a hole")

	t.Run("silent when everything posted", func(t *testing.T) {
		clean := baselineInput(t)
		clean.UnpostedRaws = map[string]int{}
		assert.Empty(t, checkByName(t, Build(clean), CheckNameTriangle).Findings)
	})
}

// TestReport_NewerThanCursorIsItsOwnRedLine pins the time gap. It is not closed
// — closing it would mean running a sync, which tops the ledger up TO the
// positions — so the one case where it is fatal gets its own line.
func TestReport_NewerThanCursorIsItsOwnRedLine(t *testing.T) {
	in := baselineInput(t)
	in.NewerThanCursor = []string{"base"}

	r := Build(in)

	verdict := checkByName(t, r, CheckNameVerdict)
	require.NotEmpty(t, verdict.Findings)
	assert.Contains(t, verdict.Findings[0], "newer than the collection cursor")
	assert.Equal(t, 1, r.ExitCode())
	assert.Equal(t, "2026-07-21T21:05:07Z", r.SyncedUntil["base"],
		"the moment of sync is printed")
	assert.Equal(t, "2026-08-01T12:00:00Z", r.PositionsFetchedAt,
		"and so is the moment the positions were taken")
}

// TestReport_CheckFailedIsGreenAndDistinctFromConvicted keeps the two knownness
// states apart. "Checked, and the answer is no" and "nobody has checked" are
// different facts with different remedies, and #58 went to some trouble to keep
// them distinguishable.
func TestReport_CheckFailedIsGreenAndDistinctFromConvicted(t *testing.T) {
	in := baselineInput(t)
	in.Knownness = map[sync.AssetKey]string{
		sync.NewAssetKey("base", "0xeb9caafc9cd52434fc906dc6ef28f24509d9b309"): string(sync.KnownnessPending),
	}

	r := Build(in)

	assert.Empty(t, r.RedRows(), "an unchecked asset is not a finding")
	var found bool
	for _, row := range r.Rows {
		if row.Contract == "0xeb9caafc9cd52434fc906dc6ef28f24509d9b309" {
			found = true
			assert.Equal(t, CategoryCheckFailed, row.Category)
			assert.Equal(t, string(sync.KnownnessPending), row.KnownnessStatus)
		}
	}
	assert.True(t, found)
}

// TestNormalizePositions_NativeSentinel pins the provider's symbol-as-address
// spelling onto MoonTrack's literal. Without it every native position would key
// as a token called "eth", show up as a red row on every chain, and the real
// native balance would look like a position the provider does not report.
func TestNormalizePositions_NativeSentinel(t *testing.T) {
	ps, err := NormalizePositions("base", []RawBalance{
		{Balance: "1.5", Token: tok("ETH", 18, "ETH")},
		{Balance: "2", Token: tok("USDC", 6, "0xABCDEF0000000000000000000000000000000001")},
		{Balance: "0", Token: tok("SPENT", 18, "0x0000000000000000000000000000000000000002")},
	})
	require.NoError(t, err)
	require.Len(t, ps, 2, "a zero balance is dropped: both sides say 'no position'")

	assert.Equal(t, sync.NativeContract, ps[0].Key.Contract,
		`the provider sends "ETH" where an address belongs; the report maps it to the literal`)
	assert.True(t, ps[0].Key.IsNative())
	assert.Equal(t, "0xabcdef0000000000000000000000000000000001", ps[1].Key.Contract,
		"token addresses are lowercased, since checksum casing would split one identity in two")
}

// TestNormalizePositions_NonAddressThatIsNotTheSentinelIsAnError keeps the
// native mapping narrow. A blanket "anything without 0x is native" would collapse
// two malformed addresses on one chain onto one inflated native position — a
// corrupted P presented as a clean one.
func TestNormalizePositions_NonAddressThatIsNotTheSentinelIsAnError(t *testing.T) {
	_, err := NormalizePositions("base", []RawBalance{
		{Balance: "1", Token: tok("AVAX", 18, "AVAX")},
	})
	require.Error(t, err, "another chain's coin is not base's native sentinel")
	assert.Contains(t, err.Error(), "whose native coin is ETH")

	_, err = NormalizePositions("some-unknown-chain", []RawBalance{
		{Balance: "1", Token: tok("ETH", 18, "ETH")},
	})
	require.Error(t, err, "the sentinel cannot be confirmed on a chain we do not know")
	assert.Contains(t, err.Error(), "unknown chain")

	// Two malformed non-addresses must not merge into one native position.
	_, err = NormalizePositions("base", []RawBalance{
		{Balance: "1", Token: tok("JUNK1", 18, "garbage-1")},
		{Balance: "2", Token: tok("JUNK2", 18, "garbage-2")},
	})
	require.Error(t, err)
}

// TestNativeSentinel_MatchesTheProductionRule guards the deliberate duplicate.
//
// The report normalizes P with its OWN table so a shared mistake cannot make the
// reconciliation agree with itself. That independence is only safe while the two
// tables say the same thing about the chains both know: silent drift would turn
// every native position on a drifted chain red for a reason that is not a defect
// in the ledger.
func TestNativeSentinel_MatchesTheProductionRule(t *testing.T) {
	require.NotEmpty(t, nativeSymbols)
	for chain, want := range nativeSymbols {
		got, ok := assetlist.NativeSymbol(chain)
		require.True(t, ok, "chain %q is in the report's table but not the pipeline's", chain)
		assert.Equal(t, want, got,
			"chain %q: the report says %q, the pipeline says %q — a deliberate duplicate "+
				"that has drifted is worse than no duplicate", chain, want, got)
	}
	assert.Equal(t, sync.NativeContract, "native",
		"the sentinel literal is what both sides key on")
}

// TestNormalizePositions_UnreadableBalanceIsAnError proves the report refuses to
// skip a position it cannot read. Silence there would print "nothing to see" for
// a position it simply failed to parse.
func TestNormalizePositions_UnreadableBalanceIsAnError(t *testing.T) {
	_, err := NormalizePositions("base", []RawBalance{
		{Balance: "not-a-number", Token: tok("BAD", 18, "0x03")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot convert balance")

	_, err = NormalizePositions("base", []RawBalance{{Balance: "1"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no token block")
}

// TestSnapshotRoundTrip proves the snapshot is a faithful second entrance: what
// is written is what is read, and the raw provider shape survives untouched.
func TestSnapshotRoundTrip(t *testing.T) {
	orig, err := LoadSnapshot(snapshotPath)
	require.NoError(t, err)

	path := t.TempDir() + "/snap.json"
	require.NoError(t, SaveSnapshot(path, orig))

	back, err := LoadSnapshot(path)
	require.NoError(t, err)

	assert.Equal(t, orig.WalletAddress, back.WalletAddress)
	assert.True(t, orig.CapturedAt.Equal(back.CapturedAt))
	assert.Equal(t, orig.ChainNames(), back.ChainNames())

	origPos, err := orig.Positions()
	require.NoError(t, err)
	backPos, err := back.Positions()
	require.NoError(t, err)
	assert.Equal(t, origPos, backPos)
}

// TestWriteTable_PrintsFourBlocksAndVerdict pins the human-readable form: four
// blocks by the number of checks, then the rows, then the verdict.
func TestWriteTable_PrintsFourBlocksAndVerdict(t *testing.T) {
	var buf bytes.Buffer
	WriteTable(&buf, Build(baselineInput(t)))
	out := buf.String()

	for _, name := range []string{
		CheckNameTriangle, CheckNameMaterialization, CheckNameAccountShape, CheckNameVerdict,
	} {
		assert.Contains(t, out, name, "the table has a block per check")
	}
	assert.Equal(t, 4, strings.Count(out, "── CHECK "))
	assert.Contains(t, out, "VERDICT: the red category is EMPTY")
	assert.Contains(t, out, "synced until:")
}

func tok(symbol string, decimals int, address string) *struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals int    `json:"decimals"`
	Address  string `json:"address"`
} {
	return &struct {
		Symbol   string `json:"symbol"`
		Name     string `json:"name"`
		Decimals int    `json:"decimals"`
		Address  string `json:"address"`
	}{Symbol: symbol, Decimals: decimals, Address: address}
}

func checkByName(t *testing.T, r Report, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found in report", name)
	return Check{}
}

func categorySet(r Report) []Category {
	seen := map[Category]bool{}
	var out []Category
	for _, row := range r.Rows {
		if !seen[row.Category] {
			seen[row.Category] = true
			out = append(out, row.Category)
		}
	}
	return out
}

func keyFor(chain, contract string) sync.AssetKey { return sync.NewAssetKey(chain, contract) }
