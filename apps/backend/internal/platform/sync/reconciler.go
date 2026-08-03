package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// deltaDustTolerance is the maximum ABSOLUTE base-unit magnitude of a
// reconciliation delta treated as rounding noise. Amounts are stored in base
// units (NUMERIC(78,0)), so a handful of them is negligible at any realistic
// decimals scale.
//
// Applied to |delta|, so the sign does not change the handling (issue #53). It
// guards against rounding, NOT against significance: whether a discrepancy
// matters is the reconciliation report's judgement (#41), not a number here.
var deltaDustTolerance = big.NewInt(10)

// Reconciler handles Phase 2: comparing transaction flows with on-chain
// balances.
//
// Like the Collector, it records NO asset metadata (#59). Its former
// extractAssetsFromPositions wrote every position into chain_assets; that table
// is gone, and a position observed during reconciliation is not an identity the
// ledger needs anyway — reconciliation only compares numbers and reports.
type Reconciler struct {
	rawTxRepo   RawTransactionRepository
	posProvider PositionDataProvider
	walletRepo  WalletRepository
	knownFilter *KnownAssetFilter
	logger      *logger.Logger
}

// NewReconciler creates a new Reconciler.
//
// knownFilter may be nil, in which case every position is reconciled — the same
// fail-open convention the transaction path uses.
func NewReconciler(
	rawTxRepo RawTransactionRepository,
	posProvider PositionDataProvider,
	walletRepo WalletRepository,
	knownFilter *KnownAssetFilter,
	log *logger.Logger,
) *Reconciler {
	return &Reconciler{
		rawTxRepo:   rawTxRepo,
		posProvider: posProvider,
		walletRepo:  walletRepo,
		knownFilter: knownFilter,
		logger:      log.WithField("component", "reconciler"),
	}
}

// ReconcileResult is what a reconciliation pass produces.
//
// It is a struct rather than a bare count because the known-asset filter (#58)
// makes "how many positions were skipped, and were they convicted or merely
// unchecked" part of the answer, not a detail. The reconciliation report (#61)
// is built on exactly this distinction: a position skipped because its asset is
// PROVEN unknown is spam handled correctly, while a position skipped because
// nobody has managed to check it yet is a queue that may be hiding a migration
// bug. Collapsing the two would destroy the only information that tells them
// apart.
type ReconcileResult struct {
	// Flagged is the number of positions whose delta or decimals disagreed.
	Flagged int

	// PositionsChecked is the number of positions actually compared.
	PositionsChecked int

	// SkippedUnknown is the number of positions excluded because their asset is
	// terminally resolved as unknown — checked, and the answer was no.
	SkippedUnknown int

	// SkippedPending is the number excluded because their asset has no verdict
	// yet — not spam, just unchecked.
	SkippedPending int

	// Excluded lists every excluded position, so the report can show them
	// rather than merely count them. "Filter silently" was rejected in the
	// decision precisely because a count with no detail cannot be investigated.
	Excluded []ExcludedPosition

	// Explained lists every position that IS in the ledger's asset universe but
	// deliberately carries no ledger balance, because a rule rejected its legs
	// (issue #60) — a protocol receipt above all.
	//
	// It is separate from Excluded because the two are different facts with
	// different remedies. Excluded means "we do not know this asset"; Explained
	// means "we know it perfectly well and chose not to book it". Merging them
	// would tell a reader to go check whether an aToken is spam.
	Explained []ExplainedPosition
}

// ExcludedPosition is one on-chain position kept out of reconciliation by the
// known-asset filter, carried far enough to be reported.
type ExcludedPosition struct {
	ChainID     string
	AssetSymbol string
	Contract    string
	Quantity    *big.Int
	Status      KnownnessStatus
	// Checked distinguishes "checked: unknown" from "could not check yet".
	Checked bool
}

// ExplainedPosition is an on-chain position whose absence from the ledger is
// accounted for by a rule, carried with the rule that accounts for it.
//
// This is the shape the reconciliation report (#61) needs for the "in P, not in
// L" category, which decision #49 established can no longer be red by default:
// after genesis was switched off that category fills up on every wallet with
// DeFi or spam, and only the rows NOTHING explains are a real finding. The
// attribution travels with the position rather than being re-derived downstream,
// so the report and the per-chain flag are reading one answer, not two
// independently computed ones that may drift apart.
type ExplainedPosition struct {
	ChainID     string
	AssetSymbol string
	Contract    string
	Quantity    *big.Int

	// Reasons names every rule that rejected a leg of this asset. Plural because
	// one asset can be rejected by different rules in different transactions —
	// an aToken is a receipt when supplied and could equally be unknown to the
	// price provider.
	Reasons []RejectionReason
}

// Reconcile compares calculated flows from raw transactions with on-chain
// positions and FLAGS every discrepancy beyond dust on the chain it occurred on.
//
// It writes nothing to the ledger. Reconciliation detects, it does not repair
// (issue #53): a delta means the position and the transaction history disagree,
// and the only honest response is to say so. The previous behaviour — booking a
// positive delta as a synthetic `genesis_balance`, i.e. income out of nowhere —
// was removed because it destroyed the very signal it was computed from, and it
// did so with a cost basis of zero that no backfill would ever revisit.
//
// Returns the reconciliation result: what was flagged, and what was excluded by
// the known-asset filter and why.
func (r *Reconciler) Reconcile(ctx context.Context, w *wallet.Wallet) (ReconcileResult, error) {
	var result ReconcileResult

	if err := r.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseReconciling)); err != nil {
		return result, fmt.Errorf("failed to set sync phase: %w", err)
	}

	// Load all raw transactions
	raws, err := r.rawTxRepo.GetAllByWallet(ctx, w.ID)
	if err != nil {
		return result, fmt.Errorf("failed to get raw transactions: %w", err)
	}

	// Calculate net flows from raw transactions, EXCLUDING every leg that a rule
	// keeps out of the ledger (issue #60). Both sides of the comparison then
	// describe the same set of assets, which is the only way the delta can mean
	// what it claims to mean.
	flow, err := calculateNetFlows(raws, newRejectionResolver(ctx, r.knownFilter, r.logger))
	if err != nil {
		return result, fmt.Errorf("failed to calculate net flows: %w", err)
	}

	r.logger.Info("calculated net flows",
		"wallet_id", w.ID,
		"assets", len(flow.flows),
		"rejected_assets", len(flow.rejected))

	// Fetch on-chain positions per enabled chain. The rows of wallet_chain_sync
	// ARE the wallet chain set (issue #27), so reconciliation iterates exactly
	// this set: the position provider is chain-aware and the Reconciler owns the
	// fan-out. Per issue #28 a per-chain fetch error is ISOLATED: mark only that
	// chain errored and skip it, then keep reconciling the healthy chains. A chain
	// whose balance failed to load contributes no positions, so nothing is
	// compared for it — and it is already flagged by the fetch failure itself.
	chainRows, err := r.walletRepo.GetChainSyncRows(ctx, w.ID)
	if err != nil {
		return result, fmt.Errorf("failed to load wallet chain set: %w", err)
	}

	var positions []OnChainPosition
	for _, cr := range chainRows {
		chainPositions, err := r.posProvider.GetPositions(ctx, w.Address, cr.Chain)
		if err != nil {
			r.logger.Warn("chain position fetch failed, isolating and continuing",
				"wallet_id", w.ID,
				"chain", cr.Chain,
				"error", err)
			if serr := r.walletRepo.SetChainSyncError(ctx, w.ID, cr.Chain,
				fmt.Sprintf("reconcile failed: %v", err)); serr != nil {
				r.logger.Error("failed to mark chain sync error",
					"wallet_id", w.ID, "chain", cr.Chain, "error", serr)
			}
			continue
		}
		positions = append(positions, chainPositions...)
	}

	r.logger.Info("fetched on-chain positions",
		"wallet_id", w.ID,
		"chains", len(chainRows),
		"positions", len(positions))

	flaggedCount := 0

	for _, pos := range positions {
		if pos.Quantity == nil || pos.Quantity.Sign() <= 0 {
			continue
		}

		// APPLICATION POINT 2 OF TWO (issue #58). Positions in unknown assets
		// take part in NEITHER the decimals check NOR the delta — but they are
		// COUNTED AND CARRIED, never dropped in silence.
		//
		// Both halves matter. Letting them through would put spam back into the
		// chain of consequences the transaction-stream filter just took it out
		// of: a spam position has no matching flow (its legs never entered the
		// ledger), so it reads as a full-size unexplained delta and flags the
		// chain, and every real discrepancy on that chain is then buried under
		// noise. Dropping them silently is equally wrong: the reconciliation
		// report (#61) distinguishes spam from a migration bug by exactly this
		// information, and a filter that erases it makes "the balance is
		// correct" unverifiable.
		if excluded, ok := r.excludePosition(ctx, pos); ok {
			result.Excluded = append(result.Excluded, excluded)
			if excluded.Checked {
				result.SkippedUnknown++
			} else {
				result.SkippedPending++
			}
			continue
		}

		posKey := NewAssetKey(pos.ChainID, pos.ContractAddress)

		// A position in an asset that some rule kept out of the ledger is
		// EXPLAINED, not discrepant (issue #60). The clearest case is a protocol
		// receipt: the aToken is a real, quoted, perfectly known asset — the
		// known-asset filter says so correctly — so the exclusion above does not
		// catch it, and its net flow is zero because the receipt rule dropped the
		// leg at the provider boundary. Comparing the two produced a delta equal
		// to the entire balance and flagged the chain, on the first sync of any
		// wallet that ever supplied to a lending market.
		//
		// It is skipped with the rule NAMED rather than silently, because that
		// attribution is the whole content of the answer: "absent from the ledger
		// because a rule excluded it" is correct behaviour, while "absent for no
		// reason anyone can state" is the one case that is genuinely red. The
		// reconciliation report (#61) makes the same distinction from the same
		// field, so the flag and the report cannot disagree about a given asset.
		assetFlow, exists := flow.flows[posKey]

		// The exemption is exact: it applies only when EVERY leg of this asset
		// was rejected, which is what `!exists` means — no leg of it was ever
		// booked, so the ledger holds no balance to compare and the position is
		// accounted for in full by the rule that rejected it.
		//
		// An asset with rejected legs AND booked legs stays under comparison.
		// Excusing it wholesale would be the same failure this ticket removes,
		// merely inverted: a single rejected leg would make the asset
		// permanently unflaggable, and a real discrepancy in the part that WAS
		// booked would be silently absorbed — "молча заклеено", the outcome
		// genesis was switched off (#49, #53) to stop producing. The delta must
		// stop counting rejected LEGS, not stop watching the asset.
		if reasons, explained := flow.explains(posKey); explained && !exists {
			r.logger.Info("position explained by leg rejection: not a discrepancy",
				"wallet_id", w.ID,
				"chain_id", pos.ChainID,
				"asset", pos.AssetSymbol,
				"contract", posKey.Contract,
				"quantity", pos.Quantity.String(),
				"rejection_reasons", reasons)
			result.Explained = append(result.Explained, ExplainedPosition{
				ChainID:     pos.ChainID,
				AssetSymbol: pos.AssetSymbol,
				Contract:    posKey.Contract,
				Quantity:    pos.Quantity,
				Reasons:     reasons,
			})
			continue
		}

		result.PositionsChecked++

		var netFlow *big.Int
		if exists {
			netFlow = assetFlow.NetFlow()
		} else {
			netFlow = big.NewInt(0)
		}

		// MT-SYNC-04: AssetFlow.Decimals is fixed from the first transfer seen and
		// never re-checked, while pos.Quantity is scaled at pos.Decimals. Summing
		// netFlow (at flow.Decimals) and subtracting pos.Quantity (at pos.Decimals)
		// only makes sense when the two scales agree. A mismatch makes the delta
		// garbage, so it is flagged as its own discrepancy and the position skipped
		// — there is no comparable delta to report.
		// Only meaningful when a flow exists (no flow => nothing to compare against).
		if exists && assetFlow.Decimals != pos.Decimals {
			r.logger.Error("decimals mismatch between calculated flow and on-chain position",
				"wallet_id", w.ID,
				"chain_id", pos.ChainID,
				"asset", pos.AssetSymbol,
				"flow_decimals", assetFlow.Decimals,
				"position_decimals", pos.Decimals)
			r.flagChain(ctx, w.ID, pos.ChainID, fmt.Sprintf(
				"decimals mismatch for %s on %s: flow=%d position=%d",
				pos.AssetSymbol, pos.ChainID, assetFlow.Decimals, pos.Decimals))
			flaggedCount++
			continue
		}

		// The delta is the whole product of reconciliation: how far the on-chain
		// position is from what the collected transactions account for. It is a
		// verdict, never an instruction to write anything.
		delta := new(big.Int).Sub(pos.Quantity, netFlow)

		// Dust is rounding noise in either direction — not a discrepancy.
		absDelta := new(big.Int).Abs(delta)
		if absDelta.Cmp(deltaDustTolerance) <= 0 {
			continue
		}

		// Beyond dust, in EITHER direction: the position and the history disagree.
		// The sign only tells us which way (on-chain over- or under-reports
		// relative to the ledger), not how seriously to take it, so both are
		// flagged identically (issue #53).
		r.logger.Warn("reconciliation delta beyond dust",
			"wallet_id", w.ID,
			"chain_id", pos.ChainID,
			"asset", pos.AssetSymbol,
			"on_chain", pos.Quantity.String(),
			"calculated", netFlow.String(),
			"delta", delta.String())

		r.flagChain(ctx, w.ID, pos.ChainID, fmt.Sprintf(
			"balance does not match transaction history for %s on %s: on_chain=%s calculated=%s delta=%s",
			pos.AssetSymbol, pos.ChainID, pos.Quantity.String(), netFlow.String(), delta.String()))
		flaggedCount++
	}

	result.Flagged = flaggedCount

	r.logger.Info("reconciliation complete",
		"wallet_id", w.ID,
		"flagged", flaggedCount,
		"positions_checked", result.PositionsChecked,
		"skipped_unknown", result.SkippedUnknown,
		"skipped_pending", result.SkippedPending)

	return result, nil
}

// newRejectionResolver returns the predicate calculateNetFlows uses to decide
// whether a leg still present in the raw is nonetheless kept out of the ledger.
//
// It re-derives the known-asset verdict (#58) rather than reading a stored
// rejection, and the reason is a matter of WHEN, not of taste: on an initial
// sync the pipeline runs collect → reconcile → process, so at reconcile time the
// processing phase that records the knownness rejection has not run for a single
// raw. A reconciler that waited for that record would be checking an empty set
// on exactly the sync the acceptance criteria are about.
//
// Re-deriving is sound here and only here because the verdict is a pure function
// of a local table: the same filter, the same key, the same answer, with no
// network call. The receipt rule is the opposite — it is applied and destroyed
// at the provider boundary — which is why that one is READ from what the adapter
// recorded, and why one uniform mechanism for both was not available.
//
// A nil filter yields a nil predicate: nothing is rejected, matching the
// fail-open convention of the transaction path.
func newRejectionResolver(
	ctx context.Context,
	filter *KnownAssetFilter,
	log *logger.Logger,
) func(chain, contract, symbol string) (RejectionReason, bool) {
	if filter == nil {
		return nil
	}

	return func(chain, contract, symbol string) (RejectionReason, bool) {
		key := NewAssetKey(chain, contract)
		verdict, err := filter.Resolve(ctx, key, symbol)
		if err != nil {
			// Fail OPEN, exactly as the ledger path does: an unreadable registry
			// must not silently shrink the flow. Counting the leg can at worst
			// produce a visible discrepancy; dropping it would produce a clean
			// reconciliation that hides one.
			log.Warn("known-asset filter failed, counting leg in net flow",
				"chain", key.Chain,
				"contract", key.Contract,
				"asset", symbol,
				"error", err)
			return "", false
		}
		if verdict.Known {
			return "", false
		}
		return RejectionUnknownAsset, true
	}
}

// excludePosition asks the known-asset filter whether a position takes part in
// reconciliation, returning the excluded record when it does not.
//
// On a filter error the position is INCLUDED: an unreadable registry must not
// quietly shrink what reconciliation covers, because the resulting silence looks
// exactly like a clean reconciliation.
func (r *Reconciler) excludePosition(ctx context.Context, pos OnChainPosition) (ExcludedPosition, bool) {
	if r.knownFilter == nil {
		return ExcludedPosition{}, false
	}

	key := NewAssetKey(pos.ChainID, pos.ContractAddress)
	verdict, err := r.knownFilter.Resolve(ctx, key, pos.AssetSymbol)
	if err != nil {
		r.logger.Warn("known-asset filter failed, reconciling position anyway",
			"chain_id", pos.ChainID,
			"asset", pos.AssetSymbol,
			"error", err)
		return ExcludedPosition{}, false
	}
	if verdict.Known {
		return ExcludedPosition{}, false
	}

	r.logger.Info("position excluded from reconciliation: asset not known",
		"chain_id", pos.ChainID,
		"asset", pos.AssetSymbol,
		"contract", key.Contract,
		"knownness_status", string(verdict.Status),
		"checked", verdict.Checked())

	return ExcludedPosition{
		ChainID:     pos.ChainID,
		AssetSymbol: pos.AssetSymbol,
		Contract:    key.Contract,
		Quantity:    pos.Quantity,
		Status:      verdict.Status,
		Checked:     verdict.Checked(),
	}, true
}

// flagChain records a reconciliation discrepancy on ONE chain (so it becomes
// visible instead of silently proceeding) without aborting the reconcile of the
// wallet's other chains (issue #28). Used for a beyond-dust delta of either sign
// (MT-SYNC-03) and for a decimals mismatch (MT-SYNC-04) — one consistent
// surfacing mechanism.
//
// The flag deliberately carries no magnitude and no threshold of its own: it says
// "here the numbers do not add up, go look", and the reconciliation report (#41)
// is what says how much and why. Two reporting surfaces that each judge
// significance would eventually disagree; one indicator plus one report cannot.
// The caller skips the offending position and continues; the wallet-level rollup
// then surfaces the error.
func (r *Reconciler) flagChain(ctx context.Context, walletID uuid.UUID, chain, reason string) {
	errMsg := "reconciliation discrepancy: " + reason
	if err := r.walletRepo.SetChainSyncError(ctx, walletID, chain, errMsg); err != nil {
		r.logger.Error("failed to mark chain sync degraded",
			"wallet_id", walletID,
			"chain", chain,
			"reason", reason,
			"error", err)
	}
}

// flowResult is what a pass over the collected raws produces: the net flow per
// asset identity, plus every leg the collected history deliberately kept out of
// the ledger.
//
// The two travel together because they answer one question between them. A
// position is explained by the flow when the numbers agree, and it is explained
// by a rejection when they do not but a rule says the asset was never meant to
// be in the ledger at all. Computing them in separate passes would let the two
// halves be built from different subsets of the same raws — the one way this
// check can silently agree with itself.
type flowResult struct {
	// flows is keyed by AssetKey: the asset's on-chain identity.
	flows map[AssetKey]*AssetFlow

	// rejected maps an asset identity to what was rejected under it.
	rejected map[AssetKey]*rejectionTally
}

// rejectionTally accumulates what the rules kept out of the ledger for one asset
// identity.
//
// It records the reasons as a SET rather than a count because the report
// attributes an absence to a rule, and one asset can be rejected by different
// rules in different transactions. It also keeps the metadata and the summed
// magnitude, because an asset whose every leg was rejected has no flow entry to
// borrow a name or a size from — and #41 requires those assets listed by name
// with quantities, not as anonymous rows.
type rejectionTally struct {
	reasons  map[RejectionReason]bool
	symbol   string
	decimals int
	amount   *big.Int
}

// explains reports whether some rule excluded a leg of this asset from the
// ledger, and which rules. The boolean is the reconciler's question; the reasons
// are the report's.
//
// It answers about REJECTION only. Whether the rejection fully accounts for the
// asset having no ledger balance is a second question — the caller must also
// establish that no leg of it was booked — and the two are kept apart so neither
// can be mistaken for the other.
//
// The reasons come out in a fixed order rather than in map order, so two runs
// over the same history produce byte-identical output; diffing two report runs
// is the main way this check is used.
func (f flowResult) explains(key AssetKey) ([]RejectionReason, bool) {
	tally, ok := f.rejected[key]
	if !ok || len(tally.reasons) == 0 {
		return nil, false
	}
	out := make([]RejectionReason, 0, len(tally.reasons))
	for _, r := range []RejectionReason{RejectionReceipt, RejectionUnknownAsset} {
		if tally.reasons[r] {
			out = append(out, r)
		}
	}
	return out, true
}

// calculateNetFlows processes raw transactions and computes the net flow per
// asset IDENTITY — (chain, contract) — together with the legs that were rejected
// from the ledger.
//
// The key is the AssetKey and emphatically not the ticker (issue #60). Keying by
// symbol let two different contracts sharing a ticker sum into one flow, and the
// measurement that produced this change found exactly that on real data: the
// wallet's real USDC on base nets to 13888232 base units, matching the on-chain
// position to the unit, while the symbol-keyed flow reported
// 25000000000018233539 because two spam contracts also called "USDC" — one of
// them with 18 decimals — were being added to the same bucket. The chain was
// flagged for a discrepancy that consisted entirely of other assets' amounts.
// The identity registry already decided this question for the ledger in #59;
// reconciliation was the last place still adding up tickers.
//
// resolveRejection, when non-nil, is asked whether a leg still present in the
// raw must nonetheless stay out of the ledger. It exists because the two
// rejection rules differ in kind. The receipt rule (#57) is applied inside the
// provider adapter, BEFORE the raw is written, so its legs are already absent
// here and can only be known from what the adapter recorded — hence
// dt.RejectedLegs. The known-asset filter (#58) runs later, in the processing
// phase, which on an initial sync has not run yet when reconciliation happens;
// its verdict is a pure function of a local table, so it is re-derived here from
// the leg that is still in the raw. Neither rule could serve the other's shape.
func calculateNetFlows(
	raws []*RawTransaction,
	resolveRejection func(chain, contract, symbol string) (RejectionReason, bool),
) (flowResult, error) {
	result := flowResult{
		flows:    make(map[AssetKey]*AssetFlow),
		rejected: make(map[AssetKey]*rejectionTally),
	}

	// markRejected records one rejected leg under its asset identity, keeping the
	// metadata and adding up the magnitude so an asset that never reaches the
	// ledger can still be reported by name and by size.
	markRejected := func(key AssetKey, reason RejectionReason, symbol string, decimals int, amount *big.Int) {
		tally, ok := result.rejected[key]
		if !ok {
			tally = &rejectionTally{
				reasons: make(map[RejectionReason]bool, 1),
				amount:  big.NewInt(0),
			}
			result.rejected[key] = tally
		}
		tally.reasons[reason] = true
		// First non-empty wins, matching how AssetFlow fixes its metadata from
		// the first leg seen.
		if tally.symbol == "" {
			tally.symbol = symbol
		}
		if tally.decimals == 0 {
			tally.decimals = decimals
		}
		if amount != nil {
			tally.amount.Add(tally.amount, new(big.Int).Abs(amount))
		}
	}

	for _, raw := range raws {
		var dt DecodedTransaction
		if err := json.Unmarshal(raw.RawJSON, &dt); err != nil {
			return flowResult{}, fmt.Errorf("failed to unmarshal raw tx %s: %w", raw.ExternalID, err)
		}

		chainID := dt.ChainID

		// Legs the adapter already rejected. They contribute NO flow — they never
		// reached the ledger — but their identity is recorded, so a position in
		// that asset is explained rather than flagged.
		for _, rl := range dt.RejectedLegs {
			chain := rl.ChainID
			if chain == "" {
				chain = chainID
			}
			markRejected(NewAssetKey(chain, rl.ContractAddress), rl.Reason,
				rl.AssetSymbol, rl.Decimals, rl.Amount)
		}

		for _, t := range dt.Transfers {
			// A leg's chain is the destination chain for the inbound side of a
			// stitched bridge — the same attribution the identity resolve and the
			// known-asset filter use, so all three agree on which chain's asset
			// this is.
			chain := chainID
			if dt.DestChainID != "" && t.Direction == DirectionIn {
				chain = dt.DestChainID
			}
			key := NewAssetKey(chain, t.ContractAddress)

			// A leg that will not enter the ledger must not enter the flow it is
			// compared against. Counting it produced the defect this ticket
			// exists for: the leg is excluded from L, still counted in F, and the
			// resulting delta flags the chain for a difference the filter created
			// deliberately.
			if resolveRejection != nil {
				if reason, rejectedLeg := resolveRejection(chain, t.ContractAddress, t.AssetSymbol); rejectedLeg {
					markRejected(key, reason, t.AssetSymbol, t.Decimals, t.Amount)
					continue
				}
			}

			flow, exists := result.flows[key]
			if !exists {
				flow = &AssetFlow{
					ChainID:         chain,
					AssetSymbol:     t.AssetSymbol,
					ContractAddress: key.Contract,
					Decimals:        t.Decimals,
					Inflow:          big.NewInt(0),
					Outflow:         big.NewInt(0),
				}
				result.flows[key] = flow
			}

			if t.Amount == nil {
				continue
			}
			if t.Direction == DirectionIn {
				flow.Inflow.Add(flow.Inflow, t.Amount)
			} else {
				flow.Outflow.Add(flow.Outflow, t.Amount)
			}
		}

		// Count fees as outflow for the native coin. Gas is paid in the chain's
		// native coin by construction, so the identity is the native sentinel and
		// not the fee's ticker — the same reason the fee leg is never filtered.
		if dt.Fee != nil && dt.Fee.Amount != nil && dt.Fee.Amount.Sign() > 0 {
			feeKey := NewAssetKey(chainID, NativeContract)
			flow, exists := result.flows[feeKey]
			if !exists {
				flow = &AssetFlow{
					ChainID:         chainID,
					AssetSymbol:     dt.Fee.AssetSymbol,
					ContractAddress: NativeContract,
					Decimals:        dt.Fee.Decimals,
					Inflow:          big.NewInt(0),
					Outflow:         big.NewInt(0),
				}
				result.flows[feeKey] = flow
			}
			flow.Outflow.Add(flow.Outflow, dt.Fee.Amount)
		}
	}

	return result, nil
}
