package sync

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Service handles blockchain wallet synchronization
type Service struct {
	config          *Config
	walletRepo      WalletRepository
	zerionProvider  TransactionDataProvider
	zerionProcessor *ZerionProcessor
	logger          *logger.Logger
	wg              sync.WaitGroup
	stopCh          chan struct{}
	mu              sync.RWMutex
	running         bool
}

// NewService creates a new sync service.
func NewService(
	config *Config,
	walletRepo WalletRepository,
	ledgerSvc LedgerService,
	assetSvc AssetService,
	logger *logger.Logger,
	zerionProvider TransactionDataProvider,
) *Service {
	if config == nil {
		config = DefaultConfig()
	}
	_ = config.Validate()

	var zerionProc *ZerionProcessor
	if zerionProvider != nil {
		zerionProc = NewZerionProcessor(walletRepo, ledgerSvc, logger)
	}

	return &Service{
		config:          config,
		walletRepo:      walletRepo,
		zerionProvider:  zerionProvider,
		zerionProcessor: zerionProc,
		logger:          logger.WithField("component", "sync"),
		stopCh:          make(chan struct{}),
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
	s.logger.Info("manual sync triggered", "wallet_id", walletID)

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

// syncWallet syncs a single wallet using the Zerion decoded transaction API (time-based cursor)
func (s *Service) syncWallet(ctx context.Context, w *wallet.Wallet) error {
	s.logger.Info("starting wallet sync",
		"wallet_id", w.ID,
		"address", w.Address,
		"chain_id", w.ChainID,
		"last_sync_at", w.LastSyncAt)

	// Atomically claim the wallet for syncing
	claimed, err := s.walletRepo.ClaimWalletForSync(ctx, w.ID)
	if err != nil {
		return fmt.Errorf("failed to claim wallet for sync: %w", err)
	}
	if !claimed {
		s.logger.Debug("wallet already being synced, skipping", "wallet_id", w.ID)
		return nil
	}

	// Determine "since" time for fetching transactions
	var since time.Time
	if w.LastSyncAt != nil {
		since = *w.LastSyncAt
	} else {
		// Initial sync — look back by configured duration
		since = time.Now().Add(-s.config.InitialSyncLookback)
	}

	s.logger.Debug("sync window", "wallet_id", w.ID, "since", since, "is_initial", w.LastSyncAt == nil)

	// Fetch decoded transactions from Zerion
	transactions, err := s.zerionProvider.GetTransactions(ctx, w.Address, w.ChainID, since)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get transactions from zerion: %v", err)
		_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
		return fmt.Errorf("failed to get transactions from zerion: %w", err)
	}

	s.logger.Info("fetched decoded transactions",
		"wallet_id", w.ID,
		"count", len(transactions),
		"since", since.Format(time.RFC3339))

	// Sort transactions oldest-first (Zerion returns newest-first).
	// Both phases benefit: minimizes negative-balance hits during initial sync,
	// ensures correct ordering during incremental sync.
	sort.Slice(transactions, func(i, j int) bool {
		return transactions[i].MinedAt.Before(transactions[j].MinedAt)
	})

	// Two-phase sync:
	// - Initial sync (LastSyncAt == nil): skip negative balance errors (missing prior history)
	// - Incremental sync: stop on any error (negative balance = real problem)
	isInitialSync := w.LastSyncAt == nil

	var lastSuccessfulMinedAt *time.Time
	var processErrors []error
	processed := 0
	skipped := 0

	for _, tx := range transactions {
		if err := s.zerionProcessor.ProcessTransaction(ctx, w, tx); err != nil {
			if isInitialSync && isNegativeBalanceError(err) {
				s.logger.Warn("skipping transaction (negative balance during initial sync)",
					"wallet_id", w.ID,
					"tx_hash", tx.TxHash,
					"zerion_id", tx.ID,
					"error", err)
				skipped++
				continue
			}

			s.logger.Error("failed to process transaction, stopping sync",
				"wallet_id", w.ID,
				"tx_hash", tx.TxHash,
				"zerion_id", tx.ID,
				"error", err)
			processErrors = append(processErrors, err)
			break
		}
		minedAt := tx.MinedAt
		lastSuccessfulMinedAt = &minedAt
		processed++
	}

	// Update cursor ONLY to last successfully committed transaction's MinedAt
	if lastSuccessfulMinedAt != nil {
		if err := s.walletRepo.SetSyncCompletedAt(ctx, w.ID, *lastSuccessfulMinedAt); err != nil {
			return fmt.Errorf("failed to mark sync completed: %w", err)
		}
		s.logger.Debug("sync cursor advanced", "wallet_id", w.ID, "new_cursor", *lastSuccessfulMinedAt)
	} else if len(processErrors) == 0 {
		// No transactions and no errors — wallet is up to date
		if err := s.walletRepo.SetSyncCompletedAt(ctx, w.ID, time.Now()); err != nil {
			return fmt.Errorf("failed to mark sync completed: %w", err)
		}
	} else {
		// First transaction failed — don't advance cursor
		errMsg := fmt.Sprintf("sync failed on first transaction: %v", processErrors[0])
		_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
		s.logger.Warn("sync error persisted", "wallet_id", w.ID, "error", processErrors[0])
	}

	// Clear address cache after each wallet sync
	s.zerionProcessor.ClearCache()

	s.logger.Info("wallet sync completed",
		"wallet_id", w.ID,
		"transactions_processed", processed,
		"transactions_skipped", skipped,
		"errors", len(processErrors),
		"is_initial_sync", isInitialSync)

	return nil
}
