package taxlot

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// WACPosition enriches PositionWAC with wallet context for the frontend.
type WACPosition struct {
	WalletID        uuid.UUID
	WalletName      string
	AccountID       uuid.UUID
	ChainID         string
	Asset           string
	TotalQuantity   *big.Int
	WeightedAvgCost *big.Int
}

// TransactionLotImpact contains all lot-related data for a transaction.
type TransactionLotImpact struct {
	AcquiredLots []*ledger.TaxLot
	Disposals    []*DisposalDetail
	HasLotImpact bool
}

// DisposalDetail enriches a LotDisposal with lot metadata for display.
type DisposalDetail struct {
	ledger.LotDisposal
	LotAsset                     string
	LotAcquiredAt                time.Time
	LotEffectiveCostBasisPerUnit *big.Int
	LotAutoSource                ledger.CostBasisSource
}

// Service provides business logic for tax lot operations.
type Service struct {
	taxLotRepo     ledger.TaxLotRepository
	ledgerRepo     ledger.Repository
	walletRepo     wallet.Repository
	logger         *logger.Logger
	lastWACRefresh time.Time
	wacRefreshMu   sync.Mutex
}

// NewService creates a new tax lot service.
func NewService(taxLotRepo ledger.TaxLotRepository, ledgerRepo ledger.Repository, walletRepo wallet.Repository, log *logger.Logger) *Service {
	return &Service{
		taxLotRepo: taxLotRepo,
		ledgerRepo: ledgerRepo,
		walletRepo: walletRepo,
		logger:     log.WithField("component", "taxlot"),
	}
}

// GetLotsByWallet returns tax lots for a wallet+asset, verifying ownership.
func (s *Service) GetLotsByWallet(ctx context.Context, userID, walletID uuid.UUID, asset string) ([]*ledger.TaxLot, error) {
	// Verify wallet ownership
	if _, err := s.verifyWalletOwnership(ctx, userID, walletID); err != nil {
		return nil, err
	}

	// Resolve wallet → accounts
	accounts, err := s.ledgerRepo.FindAccountsByWallet(ctx, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to find accounts for wallet: %w", err)
	}

	var allLots []*ledger.TaxLot
	for _, acc := range accounts {
		lots, err := s.taxLotRepo.GetLotsByAccount(ctx, acc.ID, asset)
		if err != nil {
			return nil, fmt.Errorf("failed to get lots for account %s: %w", acc.ID, err)
		}
		allLots = append(allLots, lots...)
	}

	return allLots, nil
}

// OverrideCostBasis sets a manual cost basis override on a lot, with audit trail.
// Uses a DB transaction for atomicity and FOR UPDATE lock to prevent concurrent override races.
func (s *Service) OverrideCostBasis(ctx context.Context, userID uuid.UUID, lotID uuid.UUID, costBasis *big.Int, reason string) error {
	// Begin transaction for atomicity
	txCtx, err := s.ledgerRepo.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer s.ledgerRepo.RollbackTx(txCtx)

	// Get the lot WITH row lock to prevent concurrent override races
	lot, err := s.taxLotRepo.GetTaxLotForUpdate(txCtx, lotID)
	if err != nil {
		if errors.Is(err, ledger.ErrLotNotFound) {
			return ErrLotNotFound
		}
		return fmt.Errorf("failed to get tax lot: %w", err)
	}

	// Verify ownership: lot → account → wallet → user
	if _, err := s.verifyLotOwnership(txCtx, userID, lot.AccountID); err != nil {
		return err
	}

	// Create audit trail record
	history := &ledger.LotOverrideHistory{
		ID:                uuid.New(),
		LotID:             lotID,
		PreviousCostBasis: lot.OverrideCostBasisPerUnit, // nil if first override
		NewCostBasis:      costBasis,
		Reason:            reason,
		ChangedAt:         time.Now().UTC(),
	}

	if err := s.taxLotRepo.CreateOverrideHistory(txCtx, history); err != nil {
		return fmt.Errorf("failed to create override history: %w", err)
	}

	// Apply the override
	if err := s.taxLotRepo.UpdateOverride(txCtx, lotID, costBasis, reason); err != nil {
		return fmt.Errorf("failed to update override: %w", err)
	}

	// Commit atomically
	if err := s.ledgerRepo.CommitTx(txCtx); err != nil {
		return fmt.Errorf("failed to commit override: %w", err)
	}

	s.logger.Info("cost basis override applied",
		"lot_id", lotID,
		"user_id", userID,
		"reason", reason,
	)

	if err := s.ForceRefreshWAC(ctx); err != nil {
		s.logger.Warn("failed to refresh WAC after override", "lot_id", lotID, "error", err)
	}

	return nil
}

// GetLotImpactByTransaction returns all lot acquisitions and disposals for a transaction.
func (s *Service) GetLotImpactByTransaction(ctx context.Context, userID, txID uuid.UUID) (*TransactionLotImpact, error) {
	acquired, err := s.taxLotRepo.GetLotsByTransaction(ctx, txID)
	if err != nil {
		return nil, fmt.Errorf("failed to get lots by transaction: %w", err)
	}

	rawDisposals, err := s.taxLotRepo.GetDisposalsByTransaction(ctx, txID)
	if err != nil {
		return nil, fmt.Errorf("failed to get disposals by transaction: %w", err)
	}

	if len(acquired) == 0 && len(rawDisposals) == 0 {
		return &TransactionLotImpact{HasLotImpact: false}, nil
	}

	// Verify ownership via at least one lot or disposal's lot
	ownershipVerified := false
	for _, lot := range acquired {
		if _, err := s.verifyLotOwnership(ctx, userID, lot.AccountID); err == nil {
			ownershipVerified = true
			break
		}
	}

	// Enrich disposals with lot metadata
	var disposals []*DisposalDetail
	for _, d := range rawDisposals {
		lot, err := s.taxLotRepo.GetTaxLot(ctx, d.LotID)
		if err != nil {
			return nil, fmt.Errorf("failed to get lot %s for disposal: %w", d.LotID, err)
		}

		if !ownershipVerified {
			if _, err := s.verifyLotOwnership(ctx, userID, lot.AccountID); err == nil {
				ownershipVerified = true
			}
		}

		disposals = append(disposals, &DisposalDetail{
			LotDisposal:                  *d,
			LotAsset:                     lot.Asset,
			LotAcquiredAt:                lot.AcquiredAt,
			LotEffectiveCostBasisPerUnit: lot.EffectiveCostBasisPerUnit(),
			LotAutoSource:                lot.AutoCostBasisSource,
		})
	}

	if !ownershipVerified {
		return nil, ErrLotNotOwned
	}

	return &TransactionLotImpact{
		AcquiredLots: acquired,
		Disposals:    disposals,
		HasLotImpact: len(acquired) > 0 || len(disposals) > 0,
	}, nil
}

// GetWAC returns weighted average cost positions, enriched with wallet context.
func (s *Service) GetWAC(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID) ([]WACPosition, error) {
	walletMap, accountIDs, err := s.getAccountsForUser(ctx, userID, walletID)
	if err != nil {
		return nil, err
	}

	if len(accountIDs) == 0 {
		return nil, nil
	}

	// Refresh materialized view before reading (throttled)
	if err := s.maybeRefreshWAC(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh WAC: %w", err)
	}

	rawPositions, err := s.taxLotRepo.GetWAC(ctx, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get WAC positions: %w", err)
	}

	// We need account→wallet and account→chainID mappings. Build from ledger accounts.
	accountToWallet := make(map[uuid.UUID]uuid.UUID)
	accountToChainID := make(map[uuid.UUID]string)
	for wID := range walletMap {
		accounts, err := s.ledgerRepo.FindAccountsByWallet(ctx, wID)
		if err != nil {
			return nil, fmt.Errorf("failed to find accounts for wallet %s: %w", wID, err)
		}
		for _, acc := range accounts {
			accountToWallet[acc.ID] = wID
			if acc.ChainID != nil {
				accountToChainID[acc.ID] = *acc.ChainID
			}
		}
	}

	// Enrich with wallet context (per-chain positions)
	var result []WACPosition
	for _, p := range rawPositions {
		wID, ok := accountToWallet[p.AccountID]
		if !ok {
			continue // skip if no wallet mapping (shouldn't happen)
		}
		w := walletMap[wID]
		result = append(result, WACPosition{
			WalletID:        wID,
			WalletName:      w.Name,
			AccountID:       p.AccountID,
			ChainID:         accountToChainID[p.AccountID],
			Asset:           p.Asset,
			TotalQuantity:   p.TotalQuantity,
			WeightedAvgCost: p.WeightedAvgCost,
		})
	}

	// Compute aggregated WAC per (wallet_id, asset)
	type aggKey struct {
		WalletID uuid.UUID
		Asset    string
	}
	agg := make(map[aggKey]struct {
		totalQty   *big.Int
		costSum    *big.Int // SUM(qty * wac)
		walletName string
	})
	for _, p := range result {
		k := aggKey{p.WalletID, p.Asset}
		entry, ok := agg[k]
		if !ok {
			entry.totalQty = new(big.Int)
			entry.costSum = new(big.Int)
			entry.walletName = p.WalletName
		}
		entry.totalQty.Add(entry.totalQty, p.TotalQuantity)
		entry.costSum.Add(entry.costSum, new(big.Int).Mul(p.TotalQuantity, p.WeightedAvgCost))
		agg[k] = entry
	}

	for k, v := range agg {
		wac := new(big.Int)
		if v.totalQty.Sign() > 0 {
			wac.Div(v.costSum, v.totalQty)
		}
		result = append(result, WACPosition{
			WalletID:        k.WalletID,
			WalletName:      v.walletName,
			AccountID:       uuid.Nil,
			ChainID:         "",
			Asset:           k.Asset,
			TotalQuantity:   v.totalQty,
			WeightedAvgCost: wac,
		})
	}

	return result, nil
}

// verifyLotOwnership checks lot → account → wallet → user chain.
func (s *Service) verifyLotOwnership(ctx context.Context, userID uuid.UUID, accountID uuid.UUID) (*wallet.Wallet, error) {
	account, err := s.ledgerRepo.GetAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	if account.WalletID == nil {
		return nil, ErrLotNotOwned
	}

	w, err := s.walletRepo.GetByID(ctx, *account.WalletID)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}

	if w.UserID != userID {
		return nil, ErrLotNotOwned
	}

	return w, nil
}

// verifyWalletOwnership checks that a wallet belongs to the user.
func (s *Service) verifyWalletOwnership(ctx context.Context, userID uuid.UUID, walletID uuid.UUID) (*wallet.Wallet, error) {
	w, err := s.walletRepo.GetByID(ctx, walletID)
	if err != nil {
		if errors.Is(err, wallet.ErrWalletNotFound) {
			return nil, ErrWalletNotOwned
		}
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}

	if w.UserID != userID {
		return nil, ErrWalletNotOwned
	}

	return w, nil
}

// getAccountsForUser returns a wallet lookup map and all account IDs for a user's wallets.
// If walletID is non-nil, only that wallet is included.
func (s *Service) getAccountsForUser(ctx context.Context, userID uuid.UUID, walletID *uuid.UUID) (map[uuid.UUID]*wallet.Wallet, []uuid.UUID, error) {
	var wallets []*wallet.Wallet

	if walletID != nil {
		w, err := s.verifyWalletOwnership(ctx, userID, *walletID)
		if err != nil {
			return nil, nil, err
		}
		wallets = []*wallet.Wallet{w}
	} else {
		var err error
		wallets, err = s.walletRepo.GetByUserID(ctx, userID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get wallets for user: %w", err)
		}
	}

	walletMap := make(map[uuid.UUID]*wallet.Wallet, len(wallets))
	var accountIDs []uuid.UUID

	for _, w := range wallets {
		walletMap[w.ID] = w
		accounts, err := s.ledgerRepo.FindAccountsByWallet(ctx, w.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to find accounts for wallet %s: %w", w.ID, err)
		}
		for _, acc := range accounts {
			accountIDs = append(accountIDs, acc.ID)
		}
	}

	return walletMap, accountIDs, nil
}

// ForceRefreshWAC refreshes the WAC materialized view bypassing the throttle.
func (s *Service) ForceRefreshWAC(ctx context.Context) error {
	s.wacRefreshMu.Lock()
	defer s.wacRefreshMu.Unlock()
	if err := s.taxLotRepo.RefreshWAC(ctx); err != nil {
		return err
	}
	s.lastWACRefresh = time.Now()
	return nil
}

// maybeRefreshWAC refreshes the WAC materialized view at most once every 30 seconds.
func (s *Service) maybeRefreshWAC(ctx context.Context) error {
	s.wacRefreshMu.Lock()
	defer s.wacRefreshMu.Unlock()

	if time.Since(s.lastWACRefresh) < 30*time.Second {
		return nil
	}

	if err := s.taxLotRepo.RefreshWAC(ctx); err != nil {
		return err
	}

	s.lastWACRefresh = time.Now()
	return nil
}
