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

// Collector handles Phase 1: collecting raw transactions from Zerion API
type Collector struct {
	zerionProvider TransactionDataProvider
	rawTxRepo      RawTransactionRepository
	walletRepo     WalletRepository
	config         *Config
	logger         *logger.Logger
}

// NewCollector creates a new Collector
func NewCollector(
	zerionProvider TransactionDataProvider,
	rawTxRepo RawTransactionRepository,
	walletRepo WalletRepository,
	config *Config,
	log *logger.Logger,
) *Collector {
	return &Collector{
		zerionProvider: zerionProvider,
		rawTxRepo:      rawTxRepo,
		walletRepo:     walletRepo,
		config:         config,
		logger:         log.WithField("component", "collector"),
	}
}

// CollectAll performs initial full collection of all transactions
func (c *Collector) CollectAll(ctx context.Context, w *wallet.Wallet) (int, error) {
	if err := c.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseCollecting)); err != nil {
		return 0, fmt.Errorf("failed to set sync phase: %w", err)
	}

	var since time.Time
	if c.config.InitialSyncLookback > 0 {
		since = time.Now().Add(-c.config.InitialSyncLookback)
	}
	c.logger.Info("collecting all transactions",
		"wallet_id", w.ID,
		"address", w.Address,
		"since", since)

	return c.collect(ctx, w, since)
}

// CollectIncremental collects only new transactions since last cursor
func (c *Collector) CollectIncremental(ctx context.Context, w *wallet.Wallet) (int, error) {
	if err := c.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseCollecting)); err != nil {
		return 0, fmt.Errorf("failed to set sync phase: %w", err)
	}

	var since time.Time
	if w.CollectCursorAt != nil {
		since = *w.CollectCursorAt
	} else if w.LastSyncAt != nil {
		since = *w.LastSyncAt
	} else if c.config.InitialSyncLookback > 0 {
		since = time.Now().Add(-c.config.InitialSyncLookback)
	}

	c.logger.Info("collecting incremental transactions",
		"wallet_id", w.ID,
		"address", w.Address,
		"since", since)

	return c.collect(ctx, w, since)
}

func (c *Collector) collect(ctx context.Context, w *wallet.Wallet, since time.Time) (int, error) {
	txs, err := c.zerionProvider.GetTransactions(ctx, w.Address, since)
	if err != nil {
		return 0, fmt.Errorf("failed to get transactions: %w", err)
	}

	c.logger.Info("fetched transactions from provider",
		"wallet_id", w.ID,
		"count", len(txs))

	var maxMinedAt *time.Time
	count := 0

	for _, dt := range txs {
		raw, err := decodedTxToRawTx(w.ID, dt)
		if err != nil {
			c.logger.Warn("failed to serialize transaction, skipping",
				"wallet_id", w.ID,
				"zerion_id", dt.ID,
				"error", err)
			continue
		}

		if err := c.rawTxRepo.UpsertRawTransaction(ctx, raw); err != nil {
			c.logger.Error("failed to upsert raw transaction",
				"wallet_id", w.ID,
				"zerion_id", dt.ID,
				"error", err)
			continue
		}

		count++
		if maxMinedAt == nil || dt.MinedAt.After(*maxMinedAt) {
			t := dt.MinedAt
			maxMinedAt = &t
		}
	}

	// Update collect cursor to max mined_at
	if maxMinedAt != nil {
		if err := c.walletRepo.SetCollectCursor(ctx, w.ID, *maxMinedAt); err != nil {
			return count, fmt.Errorf("failed to update collect cursor: %w", err)
		}
	}

	c.logger.Info("collection complete",
		"wallet_id", w.ID,
		"stored", count,
		"total_fetched", len(txs))

	return count, nil
}

// decodedTxToRawTx converts a DecodedTransaction to a RawTransaction for storage
func decodedTxToRawTx(walletID uuid.UUID, dt DecodedTransaction) (*RawTransaction, error) {
	rawJSON, err := json.Marshal(dt)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal decoded transaction: %w", err)
	}

	return &RawTransaction{
		WalletID:         walletID,
		ZerionID:         dt.ID,
		TxHash:           dt.TxHash,
		ChainID:          dt.ChainID,
		OperationType:    string(dt.OperationType),
		MinedAt:          dt.MinedAt,
		Status:           dt.Status,
		RawJSON:          rawJSON,
		ProcessingStatus: ProcessingStatusPending,
	}, nil
}
