package lots

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// maxPriceBaseUnits is 10^30 in scaled base units (price scaled 10^8).
// The maximum accepted human-readable price is therefore 10^22 USD, which
// guards against implausibly huge values that would corrupt PnL calculations.
var maxPriceBaseUnits *big.Int

func init() {
	maxPriceBaseUnits = new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
}

// Sentinel errors for the service layer.
var (
	ErrInvalidPrice = errors.New("price_usd must be a positive number no greater than 10^22")
	ErrLotNotFound  = errors.New("tax lot not found")
	ErrLotNotOwned  = errors.New("tax lot does not belong to this user")
)

// LotRepo is the subset of ledger.TaxLotRepository used by this service.
type LotRepo interface {
	GetTaxLotForUpdate(ctx context.Context, id uuid.UUID) (*ledger.TaxLot, error)
	UpdateOverride(ctx context.Context, lotID uuid.UUID, costBasis *big.Int, reason string) error
	MarkResolved(ctx context.Context, lotID uuid.UUID) error
	CreateOverrideHistory(ctx context.Context, history *ledger.LotOverrideHistory) error
	// ResolvePendingDisposalsForUser is used by SetManualPrice. It MUST filter
	// by user_id so a user's manual price cannot mutate another user's
	// pending disposals sharing the same (asset_id, minute_bucket).
	ResolvePendingDisposalsForUser(ctx context.Context, userID uuid.UUID, assetID uuid.UUID, at time.Time, proceedsPerUnit *big.Int) (int64, error)
}

// LedgerRepo is the subset of ledger.Repository used for transactions and account lookups.
type LedgerRepo interface {
	BeginTx(ctx context.Context) (context.Context, error)
	CommitTx(ctx context.Context) error
	RollbackTx(ctx context.Context) error
	GetAccount(ctx context.Context, id uuid.UUID) (*ledger.Account, error)
}

// WalletRepo is the subset of wallet.Repository needed for ownership verification.
type WalletRepo interface {
	GetByID(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error)
}

// AssetLookup resolves an asset UUID from a symbol (optionally chain-scoped).
// Used to thread a manual-price lot's symbol back to the asset_id that
// ResolvePendingDisposalsForUser expects. Optional: when nil, SetManualPrice
// still resolves the lot itself but skips pending-disposal resolution.
//
// Both methods are needed because GetTaxLotForUpdate does not currently
// surface the lot's chain_id. When the chain is unknown we fall back to
// GetAssetsBySymbol (plural) and accept the lookup only when exactly one
// asset matches — otherwise the symbol is ambiguous across chains and we
// can't safely scope ResolvePendingDisposalsForUser.
type AssetLookup interface {
	GetAssetBySymbol(ctx context.Context, symbol string, chainID *string) (*asset.Asset, error)
	GetAssetsBySymbol(ctx context.Context, symbol string) ([]asset.Asset, error)
}

// Svc provides business logic for the manual-price endpoint.
type Svc struct {
	repo       LotRepo
	ledger     LedgerRepo
	walletRepo WalletRepo
	assetLookup AssetLookup // optional
	log        *logger.Logger
}

// NewService creates a new lots.Svc.
func NewService(repo LotRepo, ledger LedgerRepo, walletRepo WalletRepo) *Svc {
	return &Svc{
		repo:       repo,
		ledger:     ledger,
		walletRepo: walletRepo,
	}
}

// WithAssetLookup returns a copy of the service with asset-lookup wired in.
// When present, SetManualPrice will also resolve pending disposals on the
// same (asset, minute_bucket) as the priced lot.
func (s *Svc) WithAssetLookup(lookup AssetLookup, log *logger.Logger) *Svc {
	if s == nil {
		return nil
	}
	cp := *s
	cp.assetLookup = lookup
	cp.log = log
	return &cp
}

// SetManualPrice sets the cost basis override on a lot and transitions
// price_status to 'resolved'. Intended for lots in 'unpriceable' or 'pending' state
// where the user knows the correct price.
//
// Pending-disposal resolution scope:
//
//	After the lot is resolved, the service also attempts to resolve pending
//	lot_disposals rows scoped to (this user, this asset, lot.AcquiredAt's
//	minute bucket). Disposals are bucketed on disposed_at, not acquired_at,
//	so this only resolves disposals whose disposed_at lands in the same
//	minute as the lot's acquisition — typically same-tx or same-block
//	acquire+dispose events (e.g. a flash-minted token sold in the same
//	block). Disposals of the same lot at unrelated times stay pending and
//	will be resolved later by the global PriceResolvedHook once a historical
//	spot price lands.
//
// Note: We intentionally skip writing to price_history here. The
// override_cost_basis_per_unit field + override_reason provide a sufficient audit
// trail without risking unique-constraint collisions on (asset_id, time) when
// multiple lots for the same asset are resolved at the same instant.
func (s *Svc) SetManualPrice(ctx context.Context, userID uuid.UUID, lotID uuid.UUID, priceUSD string, reason string) error {
	// Parse and validate price
	costBasis, err := money.ToBaseUnits(priceUSD, 8)
	if err != nil {
		return ErrInvalidPrice
	}
	if costBasis.Sign() <= 0 {
		return ErrInvalidPrice
	}
	if costBasis.Cmp(maxPriceBaseUnits) >= 0 {
		return ErrInvalidPrice
	}

	// Begin DB transaction for atomicity + row lock
	txCtx, err := s.ledger.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer s.ledger.RollbackTx(txCtx) //nolint:errcheck

	// Fetch lot with row lock to prevent concurrent override races
	lot, err := s.repo.GetTaxLotForUpdate(txCtx, lotID)
	if err != nil {
		if errors.Is(err, ledger.ErrLotNotFound) {
			return ErrLotNotFound
		}
		return fmt.Errorf("get tax lot: %w", err)
	}

	// Verify multi-tenant ownership: lot → account → wallet → user
	if err := s.verifyOwnership(txCtx, userID, lot.AccountID); err != nil {
		return err
	}

	// Create audit trail entry
	history := &ledger.LotOverrideHistory{
		ID:                uuid.New(),
		LotID:             lotID,
		PreviousCostBasis: lot.OverrideCostBasisPerUnit,
		NewCostBasis:      costBasis,
		Reason:            reason,
		ChangedAt:         time.Now().UTC(),
	}
	if err := s.repo.CreateOverrideHistory(txCtx, history); err != nil {
		return fmt.Errorf("create override history: %w", err)
	}

	// Apply the cost basis override
	if err := s.repo.UpdateOverride(txCtx, lotID, costBasis, reason); err != nil {
		return fmt.Errorf("update override: %w", err)
	}

	// Transition price_status to 'resolved'. If the lot is already resolved
	// (user correcting a prior manual override, for example), MarkResolved
	// updates zero rows and the repo surfaces ErrLotNotFound to indicate
	// the CAS missed — that's a benign no-op here, so swallow it.
	if err := s.repo.MarkResolved(txCtx, lotID); err != nil && !errors.Is(err, ledger.ErrLotNotFound) {
		return fmt.Errorf("mark resolved: %w", err)
	}

	// Also resolve pending disposals on the same (asset, minute_bucket).
	// User expectation is that PnL materializes immediately when they
	// supply a price — not only the lot's cost basis. Mirror the logic in
	// PriceResolvedHook (price_resolved_hook.go:56-63), but scoped to the
	// calling user to prevent cross-tenant contamination.
	//
	// Resilience: asset lookup is best-effort. The lot itself is already
	// resolved — that is the user's primary intent — so a missing asset
	// row must not fail the whole operation. Log a WARN and continue.
	if s.assetLookup != nil {
		s.resolvePendingDisposalsForLot(txCtx, userID, lot, costBasis)
	}

	return s.ledger.CommitTx(txCtx)
}

// resolvePendingDisposalsForLot looks up the asset UUID for lot's symbol and
// calls ResolvePendingDisposalsForUser on the matching minute bucket. The
// user-scoped variant is required here — this path is driven by a single
// user's manual-price action, so mutating disposals belonging to other
// tenants with overlapping (asset, minute) would be a data-integrity bug.
// Best-effort: any error is logged and swallowed, because the manual-price
// operation's primary job (resolving the lot) has already succeeded.
//
// Chain disambiguation: TaxLot.ChainID is NOT currently populated by
// GetTaxLotForUpdate (the tax_lots table has no chain_id column; chain
// lives on the lot's account). When lot.ChainID is empty we fall back to
// GetAssetsBySymbol and only proceed when exactly one asset matches the
// symbol. If the symbol exists on multiple chains we log a WARN and skip
// disposal resolution — without the chain we cannot pick the right
// asset_id, and guessing risks paying out proceeds against the wrong
// token. The user's primary intent (resolving the lot's cost basis) has
// already succeeded regardless.
//
// The time argument passed to the repo is lot.AcquiredAt. Resolution is
// bucketed on lot_disposals.disposed_at (see
// TaxLotRepository.ResolvePendingDisposalsForUser), so only disposals whose
// disposed_at falls in the same minute as the lot's acquisition are
// affected — typically same-tx acquire+dispose events (e.g. a flash mint
// that is immediately spent). Disposals at unrelated times remain pending.
func (s *Svc) resolvePendingDisposalsForLot(ctx context.Context, userID uuid.UUID, lot *ledger.TaxLot, priceUSD *big.Int) {
	a, err := s.resolveAssetForLot(ctx, lot)
	if err != nil || a == nil {
		return
	}
	n, err := s.repo.ResolvePendingDisposalsForUser(ctx, userID, a.ID, lot.AcquiredAt, priceUSD)
	if err != nil {
		if s.log != nil {
			s.log.Warn("manual price: resolve pending disposals failed",
				"lot_id", lot.ID.String(), "asset_id", a.ID.String(), "error", err)
		}
		return
	}
	if n > 0 && s.log != nil {
		s.log.Info("manual price: resolved pending disposals",
			"lot_id", lot.ID.String(), "asset_id", a.ID.String(), "count", n)
	}
}

// resolveAssetForLot maps lot.Asset (symbol) + lot.ChainID to a concrete
// asset row. When ChainID is non-empty it takes the chain-scoped fast path.
// When empty (today this is always — tax_lots has no chain_id column) it
// falls back to GetAssetsBySymbol and returns the unique match, or nil +
// a WARN log when the symbol is ambiguous or missing.
//
// Returns (nil, nil) when we chose to skip resolution; returns (nil, err)
// only for unexpected errors — but callers treat both as "skip and log".
func (s *Svc) resolveAssetForLot(ctx context.Context, lot *ledger.TaxLot) (*asset.Asset, error) {
	if lot.ChainID != "" {
		chain := lot.ChainID
		a, err := s.assetLookup.GetAssetBySymbol(ctx, lot.Asset, &chain)
		if err != nil || a == nil {
			if s.log != nil {
				s.log.Warn("manual price: asset lookup failed, skipping pending disposal resolution",
					"lot_id", lot.ID.String(), "symbol", lot.Asset, "chain_id", chain, "error", err)
			}
			return nil, nil
		}
		return a, nil
	}

	// Lot has no chain hint. Enumerate and require exactly one match.
	all, err := s.assetLookup.GetAssetsBySymbol(ctx, lot.Asset)
	if err != nil {
		if s.log != nil {
			s.log.Warn("manual price: asset lookup failed, skipping pending disposal resolution",
				"lot_id", lot.ID.String(), "symbol", lot.Asset, "error", err)
		}
		return nil, nil
	}
	switch len(all) {
	case 0:
		if s.log != nil {
			s.log.Warn("manual price: no asset matches lot symbol, skipping pending disposal resolution",
				"lot_id", lot.ID.String(), "symbol", lot.Asset)
		}
		return nil, nil
	case 1:
		a := all[0]
		return &a, nil
	default:
		chains := make([]string, 0, len(all))
		for _, a := range all {
			if a.ChainID != nil {
				chains = append(chains, *a.ChainID)
			} else {
				chains = append(chains, "native")
			}
		}
		if s.log != nil {
			s.log.Warn("manual price: symbol ambiguous across chains, skipping pending disposal resolution",
				"lot_id", lot.ID.String(), "symbol", lot.Asset, "chains", chains)
		}
		return nil, nil
	}
}

// verifyOwnership checks that the lot's account belongs to a wallet owned by userID.
func (s *Svc) verifyOwnership(ctx context.Context, userID uuid.UUID, accountID uuid.UUID) error {
	account, err := s.ledger.GetAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}

	if account.WalletID == nil {
		return ErrLotNotOwned
	}

	w, err := s.walletRepo.GetByID(ctx, *account.WalletID)
	if err != nil {
		return fmt.Errorf("get wallet: %w", err)
	}

	if w.UserID != userID {
		return ErrLotNotOwned
	}

	return nil
}
