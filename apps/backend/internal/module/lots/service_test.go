package lots_test

import (
	"context"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/lots"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// -----------------------------------------------------------------------------
// Fakes
// -----------------------------------------------------------------------------

type fakeLotRepo struct {
	mu sync.Mutex

	lot *ledger.TaxLot

	historyCreated     bool
	overrideUpdated    bool
	markResolvedCalled bool

	// ResolvePendingDisposals call capture.
	resolveDispCalls []resolveDispCall
	resolveDispCount int64
	resolveDispErr   error
}

type resolveDispCall struct {
	userID   uuid.UUID
	assetID  uuid.UUID
	at       time.Time
	proceeds *big.Int
}

func (r *fakeLotRepo) GetTaxLotForUpdate(_ context.Context, id uuid.UUID) (*ledger.TaxLot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lot == nil || r.lot.ID != id {
		return nil, ledger.ErrLotNotFound
	}
	return r.lot, nil
}

func (r *fakeLotRepo) UpdateOverride(_ context.Context, _ uuid.UUID, _ *big.Int, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrideUpdated = true
	return nil
}

func (r *fakeLotRepo) MarkResolved(_ context.Context, _ uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markResolvedCalled = true
	return nil
}

func (r *fakeLotRepo) CreateOverrideHistory(_ context.Context, _ *ledger.LotOverrideHistory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.historyCreated = true
	return nil
}

func (r *fakeLotRepo) ResolvePendingDisposalsForUser(_ context.Context, userID uuid.UUID, assetID uuid.UUID, at time.Time, proceeds *big.Int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolveDispCalls = append(r.resolveDispCalls, resolveDispCall{
		userID:   userID,
		assetID:  assetID,
		at:       at,
		proceeds: new(big.Int).Set(proceeds),
	})
	if r.resolveDispErr != nil {
		return 0, r.resolveDispErr
	}
	return r.resolveDispCount, nil
}

type fakeLedgerRepo struct {
	account *ledger.Account
}

func (l *fakeLedgerRepo) BeginTx(ctx context.Context) (context.Context, error) { return ctx, nil }
func (l *fakeLedgerRepo) CommitTx(_ context.Context) error                     { return nil }
func (l *fakeLedgerRepo) RollbackTx(_ context.Context) error                   { return nil }
func (l *fakeLedgerRepo) GetAccount(_ context.Context, _ uuid.UUID) (*ledger.Account, error) {
	return l.account, nil
}

type fakeWalletRepo struct {
	wallet *wallet.Wallet
}

func (w *fakeWalletRepo) GetByID(_ context.Context, _ uuid.UUID) (*wallet.Wallet, error) {
	return w.wallet, nil
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

// TestSetManualPrice_ResolvesPendingDisposals verifies that when a user
// manually prices a pending lot, the service also resolves pending disposals
// sharing the same (asset, minute_bucket).
func TestSetManualPrice_ResolvesPendingDisposals(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	userID := uuid.New()
	walletID := uuid.New()
	accountID := uuid.New()
	lotID := uuid.New()
	assetID := uuid.New()
	acquiredAt := time.Now().UTC().Truncate(time.Minute)

	lotRepo := &fakeLotRepo{
		lot: &ledger.TaxLot{
			ID:                   lotID,
			TransactionID:        uuid.New(),
			AccountID:            accountID,
			Asset:                assetID,
			QuantityAcquired:     big.NewInt(1000),
			QuantityRemaining:    big.NewInt(1000),
			AcquiredAt:           acquiredAt,
			AutoCostBasisPerUnit: nil,
			PriceStatus:          ledger.PriceStatusPending,
			ChainID:              "ethereum",
		},
		resolveDispCount: 3, // simulate 3 pending disposals resolved
	}
	ledgerRepo := &fakeLedgerRepo{
		account: &ledger.Account{ID: accountID, WalletID: &walletID},
	}
	walletRepo := &fakeWalletRepo{wallet: &wallet.Wallet{ID: walletID, UserID: userID}}
	svc := lots.NewService(lotRepo, ledgerRepo, walletRepo, log)

	err := svc.SetManualPrice(ctx, userID, lotID, "1.23", "manual price")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Base flow still ran
	if !lotRepo.historyCreated {
		t.Error("expected override history to be created")
	}
	if !lotRepo.overrideUpdated {
		t.Error("expected override to be updated")
	}
	if !lotRepo.markResolvedCalled {
		t.Error("expected MarkResolved to be called")
	}

	// ResolvePendingDisposalsForUser called with the calling user, the lot's
	// own asset UUID (no lookup in between, #59), and lot time. The user_id predicate is what prevents
	// cross-tenant contamination — so assert it explicitly.
	if len(lotRepo.resolveDispCalls) != 1 {
		t.Fatalf("expected 1 ResolvePendingDisposalsForUser call, got %d", len(lotRepo.resolveDispCalls))
	}
	call := lotRepo.resolveDispCalls[0]
	if call.userID != userID {
		t.Errorf("expected user_id %s, got %s", userID, call.userID)
	}
	if call.assetID != assetID {
		t.Errorf("expected asset_id %s, got %s", assetID, call.assetID)
	}
	if !call.at.Equal(acquiredAt) {
		t.Errorf("expected at %v, got %v", acquiredAt, call.at)
	}
	// 1.23 USD scaled 10^8 = 123_000_000
	if call.proceeds.String() != "123000000" {
		t.Errorf("expected proceeds 123000000, got %s", call.proceeds.String())
	}
}

// TestSetManualPrice_AssetLookupFailure_DoesNotFail verifies that when the
// asset lookup fails, the manual-price operation still succeeds (primary
// intent: resolve the lot). The pending-disposal resolution is best-effort.
