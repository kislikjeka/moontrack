package sync

import (
	"context"
	"fmt"
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
	ledgerSvc       LedgerService
	zerionProvider  TransactionDataProvider
	zerionProcessor *ZerionProcessor
	rawTxRepo       RawTransactionRepository
	posProvider     PositionDataProvider
	collector       *Collector
	reconciler      *Reconciler
	processor       *Processor
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
	posProvider PositionDataProvider,
	rawTxRepo RawTransactionRepository,
	zerionAssetRepo ZerionAssetRepository,
) *Service {
	if config == nil {
		config = DefaultConfig()
	}
	_ = config.Validate()

	var zerionProc *ZerionProcessor
	if zerionProvider != nil {
		zerionProc = NewZerionProcessor(walletRepo, ledgerSvc, logger)
	}

	svc := &Service{
		config:          config,
		walletRepo:      walletRepo,
		ledgerSvc:       ledgerSvc,
		zerionProvider:  zerionProvider,
		zerionProcessor: zerionProc,
		rawTxRepo:       rawTxRepo,
		posProvider:     posProvider,
		logger:          logger.WithField("component", "sync"),
		stopCh:          make(chan struct{}),
	}

	// Create sub-services for the 3-phase sync pipeline
	if zerionProvider != nil && rawTxRepo != nil {
		svc.collector = NewCollector(zerionProvider, rawTxRepo, walletRepo, zerionAssetRepo, config, logger)
		svc.processor = NewProcessor(rawTxRepo, walletRepo, zerionProc, ledgerSvc, logger)
	}
	if posProvider != nil && rawTxRepo != nil {
		svc.reconciler = NewReconciler(rawTxRepo, posProvider, walletRepo, zerionAssetRepo, logger)
	}

	return svc
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

// syncWallet syncs a single wallet using the two-phase sync pipeline:
// Initial sync: Collect → Reconcile → Process
// Incremental sync: Collect → Process
func (s *Service) syncWallet(ctx context.Context, w *wallet.Wallet) error {
	s.logger.Info("starting wallet sync",
		"wallet_id", w.ID,
		"address", w.Address,
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

	isInitial := w.LastSyncAt == nil

	if isInitial {
		s.logger.Info("initial sync: collect → reconcile → process", "wallet_id", w.ID)

		// Phase 1: Collect all raw transactions
		count, err := s.collector.CollectAll(ctx, w)
		if err != nil {
			errMsg := fmt.Sprintf("collect phase failed: %v", err)
			_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
			return fmt.Errorf("collect phase failed: %w", err)
		}
		s.logger.Info("collect phase complete", "wallet_id", w.ID, "collected", count)

		// Phase 2: Reconcile with on-chain balances (creates genesis raws)
		if s.reconciler != nil {
			genesisCount, err := s.reconciler.Reconcile(ctx, w)
			if err != nil {
				errMsg := fmt.Sprintf("reconcile phase failed: %v", err)
				_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
				return fmt.Errorf("reconcile phase failed: %w", err)
			}
			s.logger.Info("reconcile phase complete", "wallet_id", w.ID, "genesis_created", genesisCount)
		}

		// Phase 3: Process all in chronological order
		if err := s.processor.ProcessAll(ctx, w); err != nil {
			errMsg := fmt.Sprintf("process phase failed: %v", err)
			_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
			return fmt.Errorf("process phase failed: %w", err)
		}
	} else {
		s.logger.Info("incremental sync: collect → process", "wallet_id", w.ID)

		// Phase 1: Collect new transactions only
		count, err := s.collector.CollectIncremental(ctx, w)
		if err != nil {
			errMsg := fmt.Sprintf("collect phase failed: %v", err)
			_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
			return fmt.Errorf("collect phase failed: %w", err)
		}
		s.logger.Info("collect phase complete", "wallet_id", w.ID, "collected", count)

		// Phase 2: Process new transactions
		if err := s.processor.ProcessAll(ctx, w); err != nil {
			errMsg := fmt.Sprintf("process phase failed: %v", err)
			_ = s.walletRepo.SetSyncError(ctx, w.ID, errMsg)
			return fmt.Errorf("process phase failed: %w", err)
		}
	}

	// Reset sync phase to idle after completion
	_ = s.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseIdle))

	s.logger.Info("wallet sync completed", "wallet_id", w.ID, "is_initial", isInitial)

	return nil
}

// operationPriority returns sort priority for same-block ordering.
// Lower values sort first: inflows before outflows.
func operationPriority(op OperationType) int {
	switch op {
	case OpReceive, OpClaim, OpMint:
		return 0 // inflows first
	case OpDeposit, OpWithdraw:
		return 1 // DeFi middle
	case OpTrade, OpExecute:
		return 2 // swaps
	case OpSend, OpBurn:
		return 3 // outflows last
	default:
		return 2
	}
}
