package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Processor handles Phase 3: processing raw transactions through the ledger
type Processor struct {
	rawTxRepo       RawTransactionRepository
	walletRepo      WalletRepository
	zerionProcessor *ZerionProcessor
	ledgerSvc       LedgerService
	logger          *logger.Logger
}

// NewProcessor creates a new Processor
func NewProcessor(
	rawTxRepo RawTransactionRepository,
	walletRepo WalletRepository,
	zerionProcessor *ZerionProcessor,
	ledgerSvc LedgerService,
	log *logger.Logger,
) *Processor {
	return &Processor{
		rawTxRepo:       rawTxRepo,
		walletRepo:      walletRepo,
		zerionProcessor: zerionProcessor,
		ledgerSvc:       ledgerSvc,
		logger:          log.WithField("component", "processor"),
	}
}

// ProcessAll processes all pending raw transactions for a wallet in chronological order
func (p *Processor) ProcessAll(ctx context.Context, w *wallet.Wallet) error {
	if err := p.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseProcessing)); err != nil {
		return fmt.Errorf("failed to set sync phase: %w", err)
	}

	raws, err := p.rawTxRepo.GetPendingByWallet(ctx, w.ID)
	if err != nil {
		return fmt.Errorf("failed to get pending raw transactions: %w", err)
	}

	if len(raws) == 0 {
		p.logger.Info("no pending raw transactions", "wallet_id", w.ID)
		if err := p.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseSynced)); err != nil {
			return fmt.Errorf("failed to set sync phase: %w", err)
		}
		return nil
	}

	// Secondary sort: within same mined_at, use operationPriority (inflows before outflows)
	sort.SliceStable(raws, func(i, j int) bool {
		if !raws[i].MinedAt.Equal(raws[j].MinedAt) {
			return raws[i].MinedAt.Before(raws[j].MinedAt)
		}
		return operationPriority(OperationType(raws[i].OperationType)) < operationPriority(OperationType(raws[j].OperationType))
	})

	p.logger.Info("processing raw transactions",
		"wallet_id", w.ID,
		"count", len(raws))

	var lastSuccessfulMinedAt *time.Time
	processed := 0
	skipped := 0
	errors := 0
	consecutiveErrors := 0

	for _, raw := range raws {
		var ledgerTxID *uuid.UUID
		var processErr error

		if raw.IsSynthetic {
			ledgerTxID, processErr = p.processGenesis(ctx, w, raw)
		} else {
			ledgerTxID, processErr = p.processRegular(ctx, w, raw)
		}

		if processErr != nil {
			if isDuplicateError(processErr) {
				// Idempotent — already processed
				if err := p.rawTxRepo.MarkSkipped(ctx, raw.ID, "duplicate"); err != nil {
					p.logger.Error("failed to mark duplicate as skipped", "raw_id", raw.ID, "error", err)
				}
				skipped++
				consecutiveErrors = 0
				t := raw.MinedAt
				lastSuccessfulMinedAt = &t
				continue
			}

			p.logger.Error("failed to process raw transaction",
				"wallet_id", w.ID,
				"raw_id", raw.ID,
				"zerion_id", raw.ZerionID,
				"is_synthetic", raw.IsSynthetic,
				"error", processErr)

			if err := p.rawTxRepo.MarkError(ctx, raw.ID, processErr.Error()); err != nil {
				p.logger.Error("failed to mark error", "raw_id", raw.ID, "error", err)
			}

			errors++
			consecutiveErrors++

			if consecutiveErrors > 5 {
				p.logger.Warn("too many consecutive errors, stopping processing",
					"wallet_id", w.ID,
					"consecutive_errors", consecutiveErrors)
				break
			}
			continue
		}

		// Success
		if ledgerTxID != nil {
			if err := p.rawTxRepo.MarkProcessed(ctx, raw.ID, *ledgerTxID); err != nil {
				p.logger.Error("failed to mark processed", "raw_id", raw.ID, "error", err)
			}
		} else {
			// ProcessTransaction returned nil (e.g., skipped failed/unclassifiable tx)
			if err := p.rawTxRepo.MarkSkipped(ctx, raw.ID, "skipped by processor"); err != nil {
				p.logger.Error("failed to mark skipped", "raw_id", raw.ID, "error", err)
			}
			skipped++
		}

		consecutiveErrors = 0
		t := raw.MinedAt
		lastSuccessfulMinedAt = &t
		processed++
	}

	// Update last_sync_at cursor
	if lastSuccessfulMinedAt != nil {
		if err := p.walletRepo.SetSyncCompletedAt(ctx, w.ID, *lastSuccessfulMinedAt); err != nil {
			return fmt.Errorf("failed to update sync cursor: %w", err)
		}
	}

	if err := p.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseSynced)); err != nil {
		return fmt.Errorf("failed to set sync phase: %w", err)
	}

	// Clear address cache after processing
	p.zerionProcessor.ClearCache()

	p.logger.Info("processing complete",
		"wallet_id", w.ID,
		"processed", processed,
		"skipped", skipped,
		"errors", errors)

	return nil
}

// processGenesis processes a synthetic genesis raw transaction
func (p *Processor) processGenesis(ctx context.Context, w *wallet.Wallet, raw *RawTransaction) (*uuid.UUID, error) {
	var dt DecodedTransaction
	if err := json.Unmarshal(raw.RawJSON, &dt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal genesis tx: %w", err)
	}

	if len(dt.Transfers) == 0 {
		return nil, fmt.Errorf("genesis tx has no transfers")
	}

	t := dt.Transfers[0]
	usdRate := "0"
	if t.USDPrice != nil {
		usdRate = t.USDPrice.String()
	}

	rawData := map[string]interface{}{
		"wallet_id":   w.ID.String(),
		"chain_id":    dt.ChainID,
		"asset_id":    t.AssetSymbol,
		"amount":      t.Amount.String(),
		"decimals":    t.Decimals,
		"usd_rate":    usdRate,
		"occurred_at": raw.MinedAt.Format(time.RFC3339),
	}

	if t.ContractAddress != "" {
		rawData["contract_address"] = t.ContractAddress
	}

	externalID := raw.ZerionID

	ledgerTx, err := p.ledgerSvc.RecordTransaction(
		ctx,
		ledger.TxTypeGenesisBalance,
		"sync_genesis",
		&externalID,
		raw.MinedAt,
		rawData,
	)
	if err != nil {
		return nil, err
	}

	return &ledgerTx.ID, nil
}

// processRegular processes a regular (non-synthetic) raw transaction via ZerionProcessor
func (p *Processor) processRegular(ctx context.Context, w *wallet.Wallet, raw *RawTransaction) (*uuid.UUID, error) {
	var dt DecodedTransaction
	if err := json.Unmarshal(raw.RawJSON, &dt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal raw tx: %w", err)
	}

	err := p.zerionProcessor.ProcessTransaction(ctx, w, dt)
	if err != nil {
		return nil, err
	}

	// ZerionProcessor doesn't return the ledger tx ID directly.
	// We use a sentinel UUID to indicate success.
	sentinelID := uuid.New()
	return &sentinelID, nil
}
