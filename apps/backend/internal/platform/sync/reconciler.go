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

// Reconciler handles Phase 2: comparing transaction flows with on-chain balances
type Reconciler struct {
	rawTxRepo   RawTransactionRepository
	posProvider PositionDataProvider
	walletRepo  WalletRepository
	logger      *logger.Logger
}

// NewReconciler creates a new Reconciler
func NewReconciler(
	rawTxRepo RawTransactionRepository,
	posProvider PositionDataProvider,
	walletRepo WalletRepository,
	log *logger.Logger,
) *Reconciler {
	return &Reconciler{
		rawTxRepo:   rawTxRepo,
		posProvider: posProvider,
		walletRepo:  walletRepo,
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

	// Fetch on-chain positions
	positions, err := r.posProvider.GetPositions(ctx, w.Address)
	if err != nil {
		return 0, fmt.Errorf("failed to get on-chain positions: %w", err)
	}

	r.logger.Info("fetched on-chain positions",
		"wallet_id", w.ID,
		"positions", len(positions))

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

		delta := new(big.Int).Sub(pos.Quantity, netFlow)

		if delta.Sign() < 0 {
			r.logger.Warn("negative delta (on-chain < calculated), ignoring",
				"wallet_id", w.ID,
				"chain_id", pos.ChainID,
				"asset", pos.AssetSymbol,
				"on_chain", pos.Quantity.String(),
				"calculated", netFlow.String(),
				"delta", delta.String())
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

// calculateNetFlows processes raw transactions and computes net flows per asset.
// Key: "chain_id:asset_symbol"
func calculateNetFlows(raws []*RawTransaction) (map[string]*AssetFlow, error) {
	flows := make(map[string]*AssetFlow)

	for _, raw := range raws {
		var dt DecodedTransaction
		if err := json.Unmarshal(raw.RawJSON, &dt); err != nil {
			return nil, fmt.Errorf("failed to unmarshal raw tx %s: %w", raw.ZerionID, err)
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

// buildGenesisRaw creates a synthetic genesis RawTransaction for a missing balance delta
func buildGenesisRaw(walletID uuid.UUID, pos OnChainPosition, delta *big.Int, genesisTime time.Time) *RawTransaction {
	zerionID := fmt.Sprintf("genesis:%s:%s:%s", walletID.String(), pos.ChainID, pos.AssetSymbol)

	// Build a synthetic DecodedTransaction that the Processor can process as genesis
	genesisTx := DecodedTransaction{
		ID:            zerionID,
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
		ZerionID:         zerionID,
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
