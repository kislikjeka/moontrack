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
	ResolvePendingDisposals(ctx context.Context, assetID uuid.UUID, at time.Time, proceedsPerUnit *big.Int) (int64, error)
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
// ResolvePendingDisposals expects. Optional: when nil, SetManualPrice still
// resolves the lot itself but skips pending-disposal resolution.
type AssetLookup interface {
	GetAssetBySymbol(ctx context.Context, symbol string, chainID *string) (*asset.Asset, error)
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

	// Transition price_status to 'resolved' (no-op if already resolved)
	if err := s.repo.MarkResolved(txCtx, lotID); err != nil {
		return fmt.Errorf("mark resolved: %w", err)
	}

	// Also resolve pending disposals on the same (asset, minute_bucket).
	// User expectation is that PnL materializes immediately when they
	// supply a price — not only the lot's cost basis. Mirror the logic in
	// PriceResolvedHook (price_resolved_hook.go:56-63).
	//
	// Resilience: asset lookup is best-effort. The lot itself is already
	// resolved — that is the user's primary intent — so a missing asset
	// row must not fail the whole operation. Log a WARN and continue.
	if s.assetLookup != nil {
		s.resolvePendingDisposalsForLot(txCtx, lot, costBasis)
	}

	return s.ledger.CommitTx(txCtx)
}

// resolvePendingDisposalsForLot looks up the asset UUID for lot's symbol
// (scoped by the lot's chain when non-empty) and calls
// ResolvePendingDisposals on the matching minute bucket. Best-effort: any
// error is logged and swallowed, because the manual-price operation's
// primary job (resolving the lot) has already succeeded.
func (s *Svc) resolvePendingDisposalsForLot(ctx context.Context, lot *ledger.TaxLot, priceUSD *big.Int) {
	var chainPtr *string
	if lot.ChainID != "" {
		chain := lot.ChainID
		chainPtr = &chain
	}
	a, err := s.assetLookup.GetAssetBySymbol(ctx, lot.Asset, chainPtr)
	if err != nil || a == nil {
		if s.log != nil {
			s.log.Warn("manual price: asset lookup failed, skipping pending disposal resolution",
				"lot_id", lot.ID.String(), "symbol", lot.Asset, "error", err)
		}
		return
	}
	n, err := s.repo.ResolvePendingDisposals(ctx, a.ID, lot.AcquiredAt, priceUSD)
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
