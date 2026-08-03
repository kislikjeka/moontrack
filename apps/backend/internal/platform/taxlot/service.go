package taxlot

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// WACPosition enriches PositionWAC with wallet context for the frontend.
//
// Asset is the registry UUID carried straight through from ledger.PositionWAC
// (#59). It is not translated back to a ticker here: two same-ticker tokens
// aggregated into one WAC row is the arithmetic error the UUID key removes, and
// resolving the id to a display symbol belongs at the presentation edge, where
// there is a registry to ask.
type WACPosition struct {
	WalletID        uuid.UUID
	WalletName      string
	AccountID       uuid.UUID
	ChainID         string
	Asset           uuid.UUID
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
	// LotAsset is the disposed lot's registry UUID (#59), copied verbatim from
	// the lot. The HTTP layer decides how to display it.
	LotAsset                     uuid.UUID
	LotAcquiredAt                time.Time
	LotEffectiveCostBasisPerUnit *big.Int
	LotAutoSource                ledger.CostBasisSource
	RealizedGainLoss             *big.Int
}

// AssetDecimals answers "what scale is this asset's quantity in", keyed on the
// registry identity the lot already carries (#59).
//
// A lot's quantity and its cost basis are in different scales — base units and
// USD×10^8 — so turning (proceeds - cost) × quantity into a USD figure needs
// the asset's decimals. Before this ticket that came from money.GetDecimals on
// a ticker, a compiled-in table that answered "USDC" with one number no matter
// which of several same-ticker tokens the lot actually held. Decimals are a
// property of the registry row, so the registry is asked for them.
//
// The reference is a string because the registry lookup accepts both a UUID and
// a CoinGecko slug; callers here always pass Asset.String().
type AssetDecimals interface {
	GetDecimals(ctx context.Context, assetRef string) (int, error)
}

// Service provides business logic for tax lot operations.
type Service struct {
	taxLotRepo     ledger.TaxLotRepository
	ledgerRepo     ledger.Repository
	walletRepo     wallet.Repository
	decimals       AssetDecimals // nilable — realized PnL is then left unreported
	logger         *logger.Logger
	lastWACRefresh time.Time
	wacRefreshMu   sync.Mutex
}

// NewService creates a new tax lot service.
func NewService(taxLotRepo ledger.TaxLotRepository, ledgerRepo ledger.Repository, walletRepo wallet.Repository, decimals AssetDecimals, log *logger.Logger) *Service {
	return &Service{
		taxLotRepo: taxLotRepo,
		ledgerRepo: ledgerRepo,
		walletRepo: walletRepo,
		decimals:   decimals,
		logger:     log.WithField("component", "taxlot"),
	}
}

// GetLotsByWallet returns tax lots for a wallet+asset, verifying ownership.
//
// asset is the registry UUID (#59). uuid.Nil is not a wildcard — the repository
// filters on it literally and matches nothing — so callers that want every
// asset must not reach this method with an empty filter.
func (s *Service) GetLotsByWallet(ctx context.Context, userID, walletID uuid.UUID, asset uuid.UUID, chainID string) ([]*ledger.TaxLot, error) {
	// Verify wallet ownership
	if _, err := s.verifyWalletOwnership(ctx, userID, walletID); err != nil {
		return nil, err
	}

	// Resolve wallet → accounts
	accounts, err := s.ledgerRepo.FindAccountsByWallet(ctx, walletID)
	if err != nil {
		return nil, fmt.Errorf("failed to find accounts for wallet: %w", err)
	}

	chainMap := make(map[uuid.UUID]string, len(accounts))
	var allLots []*ledger.TaxLot
	for _, acc := range accounts {
		if acc.ChainID != nil {
			chainMap[acc.ID] = *acc.ChainID
		}
		lots, err := s.taxLotRepo.GetLotsByAccount(ctx, acc.ID, asset)
		if err != nil {
			return nil, fmt.Errorf("failed to get lots for account %s: %w", acc.ID, err)
		}
		allLots = append(allLots, lots...)
	}

	// Populate chain_id on lots from chainMap
	for _, lot := range allLots {
		lot.ChainID = chainMap[lot.AccountID]
	}

	// Filter by chain_id if specified
	if chainID != "" {
		filtered := allLots[:0]
		lowerChainID := strings.ToLower(chainID)
		for _, lot := range allLots {
			if strings.ToLower(lot.ChainID) == lowerChainID {
				filtered = append(filtered, lot)
			}
		}
		allLots = filtered
	}

	// Sort: chain grouping (when no filter) → newest first
	sort.Slice(allLots, func(i, j int) bool {
		// Group by chain when showing all chains
		if chainID == "" {
			ci := strings.ToLower(allLots[i].ChainID)
			cj := strings.ToLower(allLots[j].ChainID)
			if ci != cj {
				return ci < cj
			}
		}
		// Newest first (descending by AcquiredAt)
		if !allLots[i].AcquiredAt.Equal(allLots[j].AcquiredAt) {
			return allLots[i].AcquiredAt.After(allLots[j].AcquiredAt)
		}
		return allLots[i].CreatedAt.After(allLots[j].CreatedAt)
	})

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

		detail := &DisposalDetail{
			LotDisposal:                  *d,
			LotAsset:                     lot.Asset,
			LotAcquiredAt:                lot.AcquiredAt,
			LotEffectiveCostBasisPerUnit: lot.EffectiveCostBasisPerUnit(),
			LotAutoSource:                lot.AutoCostBasisSource,
		}

		// Compute realized gain/loss: (proceeds - cost) * qty / 10^decimals
		// Both prices are USD scaled 10^8, qty is in base units, result is USD scaled 10^8.
		// Skip pending disposals — their proceeds are still unresolved and
		// reporting a PnL of (0 - cost) would be wrong.
		proceedsResolved := d.ProceedsPerUnit != nil && d.ProceedsStatus != ledger.ProceedsStatusPending
		if proceedsResolved && lot.EffectiveCostBasisPerUnit() != nil && s.decimals != nil {
			// Decimals come from the registry row this lot's Asset points at.
			// A lookup failure leaves RealizedGainLoss nil — the same shape a
			// pending disposal produces — rather than dividing by a guessed
			// scale, which would misreport the PnL by orders of magnitude and
			// look like a real number (#59).
			decimals, err := s.decimals.GetDecimals(ctx, lot.Asset.String())
			if err != nil {
				s.logger.Warn("realized PnL skipped: decimals unresolved",
					"lot_id", lot.ID, "asset_id", lot.Asset, "error", err)
			} else {
				priceDiff := new(big.Int).Sub(d.ProceedsPerUnit, lot.EffectiveCostBasisPerUnit())
				gainLoss := new(big.Int).Mul(priceDiff, d.QuantityDisposed)
				divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
				gainLoss.Div(gainLoss, divisor)
				detail.RealizedGainLoss = gainLoss
			}
		}

		disposals = append(disposals, detail)
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

	return append(result, aggregateWACAcrossChains(result)...), nil
}

// aggregateWACAcrossChains rolls per-chain positions up into one row per
// (wallet, asset), carrying "cost unknown" through the rollup.
//
// Split out of GetWAC so the arithmetic can be tested without standing up three
// repositories: the rule that a fully pending position aggregates to a nil WAC
// rather than a zero one (#79) is the whole point of the function.
func aggregateWACAcrossChains(positions []WACPosition) []WACPosition {
	type aggKey struct {
		WalletID uuid.UUID
		Asset    uuid.UUID
	}
	agg := make(map[aggKey]struct {
		totalQty *big.Int
		costSum  *big.Int // SUM(qty * wac) over resolved positions only
		// resolvedQty is the quantity behind costSum. It is the divisor rather
		// than totalQty because a pending position contributes quantity but no
		// cost: dividing by totalQty would dilute the known cost across unknown
		// quantity and understate the WAC. Matches how migration 000027 builds
		// the view, which excludes pending lots from both sides of the ratio.
		resolvedQty *big.Int
		walletName  string
	})
	for _, p := range positions {
		k := aggKey{p.WalletID, p.Asset}
		entry, ok := agg[k]
		if !ok {
			entry.totalQty = new(big.Int)
			entry.costSum = new(big.Int)
			entry.resolvedQty = new(big.Int)
			entry.walletName = p.WalletName
		}
		entry.totalQty.Add(entry.totalQty, p.TotalQuantity)
		// Positions whose WAC is still unresolved (all underlying lots pending)
		// have a nil WeightedAvgCost; they contribute to totalQty but not to
		// costSum. Downstream aggregation returns an unresolved WAC when all
		// contributing positions are pending.
		if p.WeightedAvgCost != nil {
			entry.costSum.Add(entry.costSum, new(big.Int).Mul(p.TotalQuantity, p.WeightedAvgCost))
			entry.resolvedQty.Add(entry.resolvedQty, p.TotalQuantity)
		}
		agg[k] = entry
	}

	aggregated := make([]WACPosition, 0, len(agg))
	for k, v := range agg {
		// nil, not new(big.Int): with every contributing position pending,
		// costSum is 0 and a zero WAC would claim the whole position was
		// acquired for free — the unknown has to survive the aggregation (#79).
		var wac *big.Int
		if v.resolvedQty.Sign() > 0 {
			wac = new(big.Int).Div(v.costSum, v.resolvedQty)
		}
		aggregated = append(aggregated, WACPosition{
			WalletID:        k.WalletID,
			WalletName:      v.walletName,
			AccountID:       uuid.Nil,
			ChainID:         "",
			Asset:           k.Asset,
			TotalQuantity:   v.totalQty,
			WeightedAvgCost: wac,
		})
	}

	return aggregated
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
