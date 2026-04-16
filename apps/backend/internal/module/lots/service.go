package lots

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
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

// Svc provides business logic for the manual-price endpoint.
type Svc struct {
	repo      LotRepo
	ledger    LedgerRepo
	walletRepo WalletRepo
}

// NewService creates a new lots.Svc.
func NewService(repo LotRepo, ledger LedgerRepo, walletRepo WalletRepo) *Svc {
	return &Svc{
		repo:       repo,
		ledger:     ledger,
		walletRepo: walletRepo,
	}
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

	return s.ledger.CommitTx(txCtx)
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
