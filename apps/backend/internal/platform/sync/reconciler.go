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

	// Calculate net flows from raw transactions
	flows, err := calculateNetFlows(raws)
	if err != nil {
		return result, fmt.Errorf("failed to calculate net flows: %w", err)
	}

	r.logger.Info("calculated net flows",
		"wallet_id", w.ID,
		"assets", len(flows))

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

		result.PositionsChecked++

		flowKey := pos.ChainID + ":" + pos.AssetSymbol
		flow, exists := flows[flowKey]

		var netFlow *big.Int
		if exists {
			netFlow = flow.NetFlow()
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
		if exists && flow.Decimals != pos.Decimals {
			r.logger.Error("decimals mismatch between calculated flow and on-chain position",
				"wallet_id", w.ID,
				"chain_id", pos.ChainID,
				"asset", pos.AssetSymbol,
				"flow_decimals", flow.Decimals,
				"position_decimals", pos.Decimals)
			r.flagChain(ctx, w.ID, pos.ChainID, fmt.Sprintf(
				"decimals mismatch for %s on %s: flow=%d position=%d",
				pos.AssetSymbol, pos.ChainID, flow.Decimals, pos.Decimals))
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

// calculateNetFlows processes raw transactions and computes net flows per asset.
// Key: "chain_id:asset_symbol"
func calculateNetFlows(raws []*RawTransaction) (map[string]*AssetFlow, error) {
	flows := make(map[string]*AssetFlow)

	for _, raw := range raws {
		var dt DecodedTransaction
		if err := json.Unmarshal(raw.RawJSON, &dt); err != nil {
			return nil, fmt.Errorf("failed to unmarshal raw tx %s: %w", raw.ExternalID, err)
		}

		chainID := dt.ChainID

		for _, t := range dt.Transfers {
			key := chainID + ":" + t.AssetSymbol
			flow, exists := flows[key]
			if !exists {
				flow = &AssetFlow{
					ChainID:         chainID,
					AssetSymbol:     t.AssetSymbol,
					ContractAddress: t.ContractAddress,
					Decimals:        t.Decimals,
					Inflow:          big.NewInt(0),
					Outflow:         big.NewInt(0),
				}
				flows[key] = flow
			}

			if t.Direction == DirectionIn {
				flow.Inflow.Add(flow.Inflow, t.Amount)
			} else {
				flow.Outflow.Add(flow.Outflow, t.Amount)
			}
		}

		// Count fees as outflow for the native asset
		if dt.Fee != nil && dt.Fee.Amount != nil && dt.Fee.Amount.Sign() > 0 {
			feeKey := chainID + ":" + dt.Fee.AssetSymbol
			flow, exists := flows[feeKey]
			if !exists {
				flow = &AssetFlow{
					ChainID:     chainID,
					AssetSymbol: dt.Fee.AssetSymbol,
					Decimals:    dt.Fee.Decimals,
					Inflow:      big.NewInt(0),
					Outflow:     big.NewInt(0),
				}
				flows[feeKey] = flow
			}
			flow.Outflow.Add(flow.Outflow, dt.Fee.Amount)
		}
	}

	return flows, nil
}
