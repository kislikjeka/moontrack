package sync

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
)

// Service handles blockchain wallet synchronization
type Service struct {
	config           *Config
	blockchainClient BlockchainClient
	walletRepo       WalletRepository
	processor        *Processor
	logger           *slog.Logger
	wg               sync.WaitGroup
	stopCh           chan struct{}
	mu               sync.RWMutex
	running          bool
}

// NewService creates a new sync service
func NewService(
	config *Config,
	blockchainClient BlockchainClient,
	walletRepo WalletRepository,
	ledgerSvc LedgerService,
	assetSvc AssetService,
	logger *slog.Logger,
) *Service {
	if config == nil {
		config = DefaultConfig()
	}
	_ = config.Validate()

	processor := NewProcessor(walletRepo, ledgerSvc, assetSvc, logger)

	return &Service{
		config:           config,
		blockchainClient: blockchainClient,
		walletRepo:       walletRepo,
		processor:        processor,
		logger:           logger.With("service", "sync"),
		stopCh:           make(chan struct{}),
	}
}

// Run starts the background sync service
func (s *Service) Run(ctx context.Context) {
	if !s.config.Enabled {
		s.logger.Info("sync service is disabled")
		return
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.logger.Info("starting sync service",
		"poll_interval", s.config.PollInterval,
		"concurrent_wallets", s.config.ConcurrentWallets)

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	// Do an initial sync immediately
	s.syncAllWallets(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("sync service stopping (context done)")
			s.stop()
			return
		case <-s.stopCh:
			s.logger.Info("sync service stopping (stop signal)")
			return
		case <-ticker.C:
			s.syncAllWallets(ctx)
		}
	}
}

// Stop stops the sync service
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.stop()
}

func (s *Service) stop() {
	close(s.stopCh)
	s.wg.Wait()
	s.running = false
}

// syncAllWallets syncs all wallets that need syncing
func (s *Service) syncAllWallets(ctx context.Context) {
	wallets, err := s.walletRepo.GetWalletsForSync(ctx)
	if err != nil {
		s.logger.Error("failed to get wallets for sync", "error", err)
		return
	}

	if len(wallets) == 0 {
		s.logger.Debug("no wallets need syncing")
		return
	}

	s.logger.Info("syncing wallets", "count", len(wallets))

	// Use semaphore for concurrency control
	sem := make(chan struct{}, s.config.ConcurrentWallets)

	for _, w := range wallets {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}

		s.wg.Add(1)
		go func(w *wallet.Wallet) {
			defer s.wg.Done()
			defer func() { <-sem }()

			if err := s.syncWallet(ctx, w); err != nil {
				s.logger.Error("failed to sync wallet",
					"wallet_id", w.ID,
					"address", w.Address,
					"error", err)
			}
		}(w)
	}
}

// SyncWallet manually triggers sync for a specific wallet
func (s *Service) SyncWallet(ctx context.Context, walletID uuid.UUID) error {
	// This method can be called via API for manual sync trigger
	wallets, err := s.walletRepo.GetWalletsForSync(ctx)
	if err != nil {
		return fmt.Errorf("failed to get wallets: %w", err)
	}

	for _, w := range wallets {
		if w.ID == walletID {
			return s.syncWallet(ctx, w)
		}
	}

	return fmt.Errorf("wallet not found or not pending sync")
}

// syncWallet syncs a single wallet
func (s *Service) syncWallet(ctx context.Context, w *wallet.Wallet) error {
	s.logger.Info("starting wallet sync",
		"wallet_id", w.ID,
		"address", w.Address,
		"chain_id", w.ChainID,
		"last_sync_block", w.LastSyncBlock)

	// Atomically claim the wallet for syncing
	claimed, err := s.walletRepo.ClaimWalletForSync(ctx, w.ID)
	if err != nil {
		return fmt.Errorf("failed to claim wallet for sync: %w", err)
	}
	if !claimed {
		s.logger.Debug("wallet already being synced, skipping", "wallet_id", w.ID)
		return nil
	}

	// Get current block
	currentBlock, err := s.blockchainClient.GetCurrentBlock(ctx, w.ChainID)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get current block: %v", err)
		_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
		return fmt.Errorf("failed to get current block: %w", err)
	}

	// Determine start block
	var startBlock int64
	if w.LastSyncBlock != nil && *w.LastSyncBlock > 0 {
		startBlock = *w.LastSyncBlock + 1
	} else {
		// Initial sync - look back from current block
		startBlock = currentBlock - s.config.InitialSyncBlockLookback
		if startBlock < 0 {
			startBlock = 0
		}
	}

	// Sync in batches
	endBlock := startBlock + s.config.MaxBlocksPerSync
	if endBlock > currentBlock {
		endBlock = currentBlock
	}

	s.logger.Debug("syncing block range",
		"wallet_id", w.ID,
		"start_block", startBlock,
		"end_block", endBlock,
		"current_block", currentBlock)

	// Get transfers
	transfers, err := s.blockchainClient.GetTransfers(ctx, w.ChainID, w.Address, startBlock, endBlock)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get transfers: %v", err)
		_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
		return fmt.Errorf("failed to get transfers: %w", err)
	}

	s.logger.Info("fetched transfers",
		"wallet_id", w.ID,
		"count", len(transfers),
		"block_range", fmt.Sprintf("%d-%d", startBlock, endBlock))

	// Process each transfer
	var processErrors []error
	for _, transfer := range transfers {
		if err := s.processor.ProcessTransfer(ctx, w, transfer); err != nil {
			s.logger.Error("failed to process transfer",
				"wallet_id", w.ID,
				"tx_hash", transfer.TxHash,
				"error", err)
			processErrors = append(processErrors, err)
		}
	}

	// Mark sync as completed (even with some errors, we update the block pointer)
	syncTime := time.Now()
	if err := s.walletRepo.SetSyncCompleted(ctx, w.ID, endBlock, syncTime); err != nil {
		return fmt.Errorf("failed to mark sync completed: %w", err)
	}

	if len(processErrors) > 0 {
		s.logger.Warn("sync completed with errors",
			"wallet_id", w.ID,
			"error_count", len(processErrors))
	} else {
		s.logger.Info("wallet sync completed",
			"wallet_id", w.ID,
			"transfers_processed", len(transfers),
			"last_block", endBlock)
	}

	return nil
}
