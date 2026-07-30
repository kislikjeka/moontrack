package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Collector handles Phase 1: collecting raw transactions from the sync provider
type Collector struct {
	txProvider TransactionDataProvider
	rawTxRepo  RawTransactionRepository
	walletRepo WalletRepository
	assetRepo  SyncAssetRepository
	config     *Config
	logger     *logger.Logger
}

// NewCollector creates a new Collector
func NewCollector(
	txProvider TransactionDataProvider,
	rawTxRepo RawTransactionRepository,
	walletRepo WalletRepository,
	assetRepo SyncAssetRepository,
	config *Config,
	log *logger.Logger,
) *Collector {
	return &Collector{
		txProvider: txProvider,
		rawTxRepo:  rawTxRepo,
		walletRepo: walletRepo,
		assetRepo:  assetRepo,
		config:     config,
		logger:     log.WithField("component", "collector"),
	}
}

// CollectAll performs initial full collection of all transactions.
func (c *Collector) CollectAll(ctx context.Context, w *wallet.Wallet) (int, error) {
	return c.collect(ctx, w, true)
}

// CollectIncremental collects only new transactions since each chain's cursor.
func (c *Collector) CollectIncremental(ctx context.Context, w *wallet.Wallet) (int, error) {
	return c.collect(ctx, w, false)
}

// chainSince computes the `since` window for a single chain, per issue #28: each
// chain resumes from ITS OWN collect cursor, so a chain that fell behind (e.g.
// failed last cycle) can never be dragged forward past its history by a faster
// sibling. When the chain has no cursor yet, we fall back to the wallet's last
// sync (incremental) and finally to the initial-lookback window.
func (c *Collector) chainSince(w *wallet.Wallet, cr wallet.WalletChainSync, isInitial bool) time.Time {
	if cr.CollectCursorAt != nil {
		return *cr.CollectCursorAt
	}
	if !isInitial && w.LastSyncAt != nil {
		return *w.LastSyncAt
	}
	if c.config.InitialSyncLookback > 0 {
		return time.Now().Add(-c.config.InitialSyncLookback)
	}
	return time.Time{}
}

// collect fans out over the wallet's chain set (the rows of wallet_chain_sync ARE
// the chains this wallet syncs, issue #27) and collects each chain INDEPENDENTLY
// (issue #28): every chain resumes from its own cursor and a chain that errors is
// isolated — its row is marked error, its cursor is left untouched (so it resumes
// where it left off next cycle), and the loop continues to the remaining chains.
// A single chain's failure never aborts the others or corrupts their state.
func (c *Collector) collect(ctx context.Context, w *wallet.Wallet, isInitial bool) (int, error) {
	if err := c.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseCollecting)); err != nil {
		return 0, fmt.Errorf("failed to set sync phase: %w", err)
	}

	chainRows, err := c.walletRepo.GetChainSyncRows(ctx, w.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to load wallet chain set: %w", err)
	}

	count := 0
	fetched := 0
	failedChains := 0

	for _, cr := range chainRows {
		since := c.chainSince(w, cr, isInitial)

		c.logger.Info("collecting chain transactions",
			"wallet_id", w.ID,
			"address", w.Address,
			"chain", cr.Chain,
			"since", since,
			"is_initial", isInitial)

		chainTxs, err := c.txProvider.GetTransactions(ctx, w.Address, cr.Chain, since)
		if err != nil {
			// Failure isolation: mark ONLY this chain errored and move on. The
			// chain's cursor is deliberately left untouched so it resumes from its
			// own high-water mark next cycle without skipping or re-fetching others.
			c.logger.Warn("chain collect failed, isolating and continuing",
				"wallet_id", w.ID,
				"chain", cr.Chain,
				"error", err)
			if serr := c.walletRepo.SetChainSyncError(ctx, w.ID, cr.Chain,
				fmt.Sprintf("collect failed: %v", err)); serr != nil {
				c.logger.Error("failed to mark chain sync error",
					"wallet_id", w.ID, "chain", cr.Chain, "error", serr)
			}
			failedChains++
			continue
		}

		// Extract asset metadata for this chain's batch before storing raw txs.
		c.extractAssets(ctx, chainTxs)
		fetched += len(chainTxs)

		var maxMinedAt *time.Time
		for _, dt := range chainTxs {
			raw, err := decodedTxToRawTx(w.ID, dt)
			if err != nil {
				c.logger.Warn("failed to serialize transaction, skipping",
					"wallet_id", w.ID,
					"chain", cr.Chain,
					"external_id", dt.ID,
					"error", err)
				continue
			}

			if err := c.rawTxRepo.UpsertRawTransaction(ctx, raw); err != nil {
				c.logger.Error("failed to upsert raw transaction",
					"wallet_id", w.ID,
					"chain", cr.Chain,
					"external_id", dt.ID,
					"error", err)
				continue
			}

			count++
			if maxMinedAt == nil || dt.MinedAt.After(*maxMinedAt) {
				t := dt.MinedAt
				maxMinedAt = &t
			}
		}

		// Advance ONLY this chain's collect cursor to its high-water mark. Nothing
		// wallet-level: each chain owns its own resumption point (issue #28).
		if maxMinedAt != nil {
			if err := c.walletRepo.SetChainCollectCursor(ctx, w.ID, cr.Chain, *maxMinedAt); err != nil {
				c.logger.Error("failed to update chain collect cursor",
					"wallet_id", w.ID, "chain", cr.Chain, "error", err)
			}
		}
	}

	c.logger.Info("collection complete",
		"wallet_id", w.ID,
		"chains", len(chainRows),
		"failed_chains", failedChains,
		"stored", count,
		"total_fetched", fetched)

	return count, nil
}

// extractAssets iterates over decoded transactions and upserts asset metadata
// into the chain_assets table. Deduplicates by symbol:chain within the batch.
func (c *Collector) extractAssets(ctx context.Context, txs []DecodedTransaction) {
	if c.assetRepo == nil {
		return
	}

	type assetKey struct {
		symbol  string
		chainID string
	}
	seen := make(map[assetKey]bool)

	for _, dt := range txs {
		for _, t := range dt.Transfers {
			if t.AssetSymbol == "" {
				continue
			}
			key := assetKey{t.AssetSymbol, dt.ChainID}
			if seen[key] {
				continue
			}
			seen[key] = true

			if err := c.assetRepo.Upsert(ctx, &SyncAsset{
				Symbol:          t.AssetSymbol,
				Name:            t.AssetName,
				ChainID:         dt.ChainID,
				ContractAddress: t.ContractAddress,
				Decimals:        t.Decimals,
				IconURL:         t.IconURL,
			}); err != nil {
				c.logger.Warn("failed to upsert sync asset",
					"symbol", t.AssetSymbol,
					"chain_id", dt.ChainID,
					"error", err)
			}
		}

		if dt.Fee != nil && dt.Fee.AssetSymbol != "" {
			key := assetKey{dt.Fee.AssetSymbol, dt.ChainID}
			if !seen[key] {
				seen[key] = true
				if err := c.assetRepo.Upsert(ctx, &SyncAsset{
					Symbol:   dt.Fee.AssetSymbol,
					Name:     dt.Fee.AssetName,
					ChainID:  dt.ChainID,
					Decimals: dt.Fee.Decimals,
					IconURL:  dt.Fee.IconURL,
				}); err != nil {
					c.logger.Warn("failed to upsert sync asset (fee)",
						"symbol", dt.Fee.AssetSymbol,
						"chain_id", dt.ChainID,
						"error", err)
				}
			}
		}
	}
}

// decodedTxToRawTx converts a DecodedTransaction to a RawTransaction for storage
func decodedTxToRawTx(walletID uuid.UUID, dt DecodedTransaction) (*RawTransaction, error) {
	rawJSON, err := json.Marshal(dt)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal decoded transaction: %w", err)
	}

	return &RawTransaction{
		WalletID:         walletID,
		ExternalID:       dt.ID,
		TxHash:           dt.TxHash,
		ChainID:          dt.ChainID,
		OperationType:    string(dt.OperationType),
		MinedAt:          dt.MinedAt,
		Status:           dt.Status,
		RawJSON:          rawJSON,
		ProcessingStatus: ProcessingStatusPending,
	}, nil
}
