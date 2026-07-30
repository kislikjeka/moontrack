package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// negativeDeltaDustTolerance is the maximum absolute base-unit magnitude of a
// negative reconciliation delta (on-chain < calculated) that is treated as
// rounding noise and skipped. Amounts are stored in base units (NUMERIC(78,0)),
// so a handful of base units is negligible dust at any realistic decimals scale.
// Anything strictly larger is surfaced as a degraded sync rather than swallowed.
var negativeDeltaDustTolerance = big.NewInt(10)

// Reconciler handles Phase 2: comparing transaction flows with on-chain balances
type Reconciler struct {
	rawTxRepo   RawTransactionRepository
	posProvider PositionDataProvider
	walletRepo  WalletRepository
	assetRepo   SyncAssetRepository
	logger      *logger.Logger
}

// NewReconciler creates a new Reconciler
func NewReconciler(
	rawTxRepo RawTransactionRepository,
	posProvider PositionDataProvider,
	walletRepo WalletRepository,
	assetRepo SyncAssetRepository,
	log *logger.Logger,
) *Reconciler {
	return &Reconciler{
		rawTxRepo:   rawTxRepo,
		posProvider: posProvider,
		walletRepo:  walletRepo,
		assetRepo:   assetRepo,
		logger:      log.WithField("component", "reconciler"),
	}
}

// Reconcile compares calculated flows from raw transactions with on-chain positions.
// For any positive delta (on-chain > calculated), it creates a single synthetic genesis.
func (r *Reconciler) Reconcile(ctx context.Context, w *wallet.Wallet) (int, error) {
	if err := r.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseReconciling)); err != nil {
		return 0, fmt.Errorf("failed to set sync phase: %w", err)
	}

	// Delete old synthetic raw transactions before re-reconciling
	if err := r.rawTxRepo.DeleteSyntheticByWallet(ctx, w.ID); err != nil {
		return 0, fmt.Errorf("failed to delete old synthetics: %w", err)
	}

	// Load all raw transactions
	raws, err := r.rawTxRepo.GetAllByWallet(ctx, w.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to get raw transactions: %w", err)
	}

	// Calculate net flows from raw transactions
	flows, err := calculateNetFlows(raws)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate net flows: %w", err)
	}

	r.logger.Info("calculated net flows",
		"wallet_id", w.ID,
		"assets", len(flows))

	// Fetch on-chain positions per enabled chain. The rows of wallet_chain_sync
	// ARE the wallet chain set (issue #27), so reconciliation iterates exactly
	// this set: the position provider is chain-aware and the Reconciler owns the
	// fan-out. Per issue #28 a per-chain fetch error is ISOLATED: mark only that
	// chain errored and skip it, then keep reconciling the healthy chains. A chain
	// whose balance failed to load contributes no positions, so no genesis is
	// fabricated for it (genesis can only ADD, so a missing balance is safe to
	// skip); its sibling chains still reconcile and record ledger data.
	chainRows, err := r.walletRepo.GetChainSyncRows(ctx, w.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to load wallet chain set: %w", err)
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

	// Extract and upsert asset metadata from positions
	r.extractAssetsFromPositions(ctx, positions)

	// Get earliest mined_at for genesis timestamp
	earliestMinedAt, err := r.rawTxRepo.GetEarliestMinedAt(ctx, w.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to get earliest mined_at: %w", err)
	}

	// Default genesis time if no raw transactions exist
	genesisTime := time.Now().Add(-24 * time.Hour)
	if earliestMinedAt != nil {
		genesisTime = earliestMinedAt.Add(-1 * time.Second)
	}

	genesisCount := 0

	for _, pos := range positions {
		if pos.Quantity == nil || pos.Quantity.Sign() <= 0 {
			continue
		}

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
		// garbage, so treat it as a hard reconciliation error and do NOT synthesize.
		// Only meaningful when a flow exists (no flow => nothing to compare against).
		if exists && flow.Decimals != pos.Decimals {
			r.logger.Error("decimals mismatch between calculated flow and on-chain position",
				"wallet_id", w.ID,
				"chain_id", pos.ChainID,
				"asset", pos.AssetSymbol,
				"flow_decimals", flow.Decimals,
				"position_decimals", pos.Decimals)
			r.markChainDegraded(ctx, w.ID, pos.ChainID, fmt.Sprintf(
				"decimals mismatch for %s on %s: flow=%d position=%d",
				pos.AssetSymbol, pos.ChainID, flow.Decimals, pos.Decimals))
			continue
		}

		delta := new(big.Int).Sub(pos.Quantity, netFlow)

		if delta.Sign() < 0 {
			// MT-SYNC-03: on-chain balance is LESS than what our transaction history
			// calculated. Genesis can only ADD to a balance, so we cannot correct an
			// over-report here; if swallowed it becomes a permanent, invisible error.
			r.logger.Warn("negative delta (on-chain < calculated)",
				"wallet_id", w.ID,
				"chain_id", pos.ChainID,
				"asset", pos.AssetSymbol,
				"on_chain", pos.Quantity.String(),
				"calculated", netFlow.String(),
				"delta", delta.String())

			// Tolerate dust (rounding noise): a negative delta whose absolute value is
			// within a tiny hardcoded base-unit threshold is treated as noise and skipped.
			// Anything beyond that is a real discrepancy → mark the sync degraded.
			absDelta := new(big.Int).Abs(delta)
			if absDelta.Cmp(negativeDeltaDustTolerance) <= 0 {
				continue
			}

			r.markChainDegraded(ctx, w.ID, pos.ChainID, fmt.Sprintf(
				"on-chain balance below calculated for %s on %s: on_chain=%s calculated=%s delta=%s",
				pos.AssetSymbol, pos.ChainID, pos.Quantity.String(), netFlow.String(), delta.String()))
			continue
		}

		if delta.Sign() == 0 {
			continue // Complete history, no genesis needed
		}

		// Create synthetic genesis raw transaction
		raw := buildGenesisRaw(w.ID, pos, delta, genesisTime)
		if err := r.rawTxRepo.UpsertRawTransaction(ctx, raw); err != nil {
			r.logger.Error("failed to upsert genesis raw transaction",
				"wallet_id", w.ID,
				"chain_id", pos.ChainID,
				"asset", pos.AssetSymbol,
				"error", err)
			continue
		}

		genesisCount++
		r.logger.Info("created genesis raw transaction",
			"wallet_id", w.ID,
			"chain_id", pos.ChainID,
			"asset", pos.AssetSymbol,
			"delta", delta.String())
	}

	r.logger.Info("reconciliation complete",
		"wallet_id", w.ID,
		"genesis_created", genesisCount,
		"positions_checked", len(positions))

	return genesisCount, nil
}

// markChainDegraded records a reconciliation discrepancy on ONE chain (so it
// becomes visible instead of silently proceeding) without aborting the reconcile
// of the wallet's other chains (issue #28). Used for both a beyond-dust negative
// delta (MT-SYNC-03) and a decimals mismatch (MT-SYNC-04) — one consistent
// surfacing mechanism. The caller skips synthesizing genesis for the offending
// position and continues; the wallet-level rollup then surfaces the error.
func (r *Reconciler) markChainDegraded(ctx context.Context, walletID uuid.UUID, chain, reason string) {
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

// extractAssetsFromPositions upserts asset metadata from on-chain positions
func (r *Reconciler) extractAssetsFromPositions(ctx context.Context, positions []OnChainPosition) {
	if r.assetRepo == nil {
		return
	}

	for _, pos := range positions {
		if pos.AssetSymbol == "" {
			continue
		}
		if err := r.assetRepo.Upsert(ctx, &SyncAsset{
			Symbol:          pos.AssetSymbol,
			Name:            pos.AssetName,
			ChainID:         pos.ChainID,
			ContractAddress: pos.ContractAddress,
			Decimals:        pos.Decimals,
			IconURL:         pos.IconURL,
		}); err != nil {
			r.logger.Warn("failed to upsert sync asset from position",
				"symbol", pos.AssetSymbol,
				"chain_id", pos.ChainID,
				"error", err)
		}
	}
}

// buildGenesisRaw creates a synthetic genesis RawTransaction for a missing balance delta
func buildGenesisRaw(walletID uuid.UUID, pos OnChainPosition, delta *big.Int, genesisTime time.Time) *RawTransaction {
	externalID := fmt.Sprintf("genesis:%s:%s:%s", walletID.String(), pos.ChainID, pos.AssetSymbol)

	// Build a synthetic DecodedTransaction that the Processor can process as genesis
	genesisTx := DecodedTransaction{
		ID:            externalID,
		TxHash:        fmt.Sprintf("genesis_%s_%s", pos.ChainID, pos.AssetSymbol),
		ChainID:       pos.ChainID,
		OperationType: OpReceive,
		Transfers: []DecodedTransfer{
			{
				AssetSymbol:     pos.AssetSymbol,
				ContractAddress: pos.ContractAddress,
				Decimals:        pos.Decimals,
				Amount:          delta,
				Direction:       DirectionIn,
			},
		},
		MinedAt: genesisTime,
		Status:  "confirmed",
	}

	// Add USD price if available
	if pos.USDPrice != nil {
		genesisTx.Transfers[0].USDPrice = pos.USDPrice
	}

	rawJSON, _ := json.Marshal(genesisTx)

	return &RawTransaction{
		WalletID:         walletID,
		ExternalID:       externalID,
		TxHash:           genesisTx.TxHash,
		ChainID:          pos.ChainID,
		OperationType:    string(OpReceive),
		MinedAt:          genesisTime,
		Status:           "confirmed",
		RawJSON:          rawJSON,
		ProcessingStatus: ProcessingStatusPending,
		IsSynthetic:      true,
	}
}
