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
	"github.com/kislikjeka/moontrack/internal/platform/asset"
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
	resolveDispCalls  []resolveDispCall
	resolveDispCount  int64
	resolveDispErr    error
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

type fakeAssetLookup struct {
	a     *asset.Asset
	err   error
	calls []assetLookupCall

	// multi drives the plural GetAssetsBySymbol path. When non-nil it
	// supersedes `a` and is returned verbatim (useful for exercising the
	// "ambiguous symbol" and "exactly one match" branches).
	multi    []asset.Asset
	multiErr error

	pluralCalls []string
}

type assetLookupCall struct {
	symbol  string
	chainID *string
}

func (f *fakeAssetLookup) GetAssetBySymbol(_ context.Context, symbol string, chainID *string) (*asset.Asset, error) {
	f.calls = append(f.calls, assetLookupCall{symbol: symbol, chainID: chainID})
	if f.err != nil {
		return nil, f.err
	}
	return f.a, nil
}

func (f *fakeAssetLookup) GetAssetsBySymbol(_ context.Context, symbol string) ([]asset.Asset, error) {
	f.pluralCalls = append(f.pluralCalls, symbol)
	if f.multiErr != nil {
		return nil, f.multiErr
	}
	if f.multi != nil {
		return f.multi, nil
	}
	// Default: mirror the singular fake. Return a single-element slice when
	// `a` is set, else an empty slice.
	if f.a != nil {
		return []asset.Asset{*f.a}, nil
	}
	return nil, nil
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

// TestSetManualPrice_ResolvesPendingDisposals verifies that when a user
// manually prices a pending lot, the service also resolves pending disposals
// sharing the same (asset, minute_bucket). This is BUG 2 from the cycle-2
// missing-price review.
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
			Asset:                "FOO",
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
	assetLookup := &fakeAssetLookup{a: &asset.Asset{ID: assetID, Symbol: "FOO"}}

	svc := lots.NewService(lotRepo, ledgerRepo, walletRepo).WithAssetLookup(assetLookup, log)

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

	// Asset lookup occurred with the lot's symbol + chain
	if len(assetLookup.calls) != 1 {
		t.Fatalf("expected 1 asset lookup call, got %d", len(assetLookup.calls))
	}
	if assetLookup.calls[0].symbol != "FOO" {
		t.Errorf("expected symbol FOO, got %s", assetLookup.calls[0].symbol)
	}
	if assetLookup.calls[0].chainID == nil || *assetLookup.calls[0].chainID != "ethereum" {
		t.Errorf("expected chainID ethereum, got %v", assetLookup.calls[0].chainID)
	}

	// ResolvePendingDisposalsForUser called with the calling user, resolved
	// asset UUID, and lot time. The user_id predicate is what prevents
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
func TestSetManualPrice_AssetLookupFailure_DoesNotFail(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	userID := uuid.New()
	walletID := uuid.New()
	accountID := uuid.New()
	lotID := uuid.New()

	lotRepo := &fakeLotRepo{
		lot: &ledger.TaxLot{
			ID:                lotID,
			TransactionID:     uuid.New(),
			AccountID:         accountID,
			Asset:             "UNKNOWN",
			QuantityAcquired:  big.NewInt(100),
			QuantityRemaining: big.NewInt(100),
			AcquiredAt:        time.Now().UTC().Truncate(time.Minute),
			PriceStatus:       ledger.PriceStatusPending,
		},
	}
	ledgerRepo := &fakeLedgerRepo{
		account: &ledger.Account{ID: accountID, WalletID: &walletID},
	}
	walletRepo := &fakeWalletRepo{wallet: &wallet.Wallet{ID: walletID, UserID: userID}}
	// Return nil, nil (not found) — asset lookup returns nothing.
	assetLookup := &fakeAssetLookup{a: nil}

	svc := lots.NewService(lotRepo, ledgerRepo, walletRepo).WithAssetLookup(assetLookup, log)

	err := svc.SetManualPrice(ctx, userID, lotID, "5.00", "test")
	if err != nil {
		t.Fatalf("expected success when asset lookup returns nothing, got: %v", err)
	}

	if !lotRepo.markResolvedCalled {
		t.Error("expected MarkResolved to still be called despite asset lookup miss")
	}
	if len(lotRepo.resolveDispCalls) != 0 {
		t.Errorf("expected 0 ResolvePendingDisposals calls when asset lookup fails, got %d",
			len(lotRepo.resolveDispCalls))
	}
}

// TestSetManualPrice_NoAssetLookup_BackwardsCompat verifies that when the
// service is constructed without WithAssetLookup (old wiring), it still
// works for the lot-only case without touching pending disposals.
func TestSetManualPrice_NoAssetLookup_BackwardsCompat(t *testing.T) {
	ctx := context.Background()

	userID := uuid.New()
	walletID := uuid.New()
	accountID := uuid.New()
	lotID := uuid.New()

	lotRepo := &fakeLotRepo{
		lot: &ledger.TaxLot{
			ID:                lotID,
			TransactionID:     uuid.New(),
			AccountID:         accountID,
			Asset:             "BAR",
			QuantityAcquired:  big.NewInt(100),
			QuantityRemaining: big.NewInt(100),
			AcquiredAt:        time.Now().UTC(),
			PriceStatus:       ledger.PriceStatusUnpriceable,
		},
	}
	ledgerRepo := &fakeLedgerRepo{
		account: &ledger.Account{ID: accountID, WalletID: &walletID},
	}
	walletRepo := &fakeWalletRepo{wallet: &wallet.Wallet{ID: walletID, UserID: userID}}

	// NO WithAssetLookup — pre-fix wiring.
	svc := lots.NewService(lotRepo, ledgerRepo, walletRepo)

	err := svc.SetManualPrice(ctx, userID, lotID, "10", "no lookup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !lotRepo.markResolvedCalled {
		t.Error("expected MarkResolved to be called")
	}
	if len(lotRepo.resolveDispCalls) != 0 {
		t.Errorf("expected 0 ResolvePendingDisposals calls without lookup, got %d",
			len(lotRepo.resolveDispCalls))
	}
}

// TestSetManualPrice_AmbiguousSymbol_SkipsDisposalResolution verifies that
// when the lot has no chain hint (ChainID == ""), and the symbol exists on
// more than one chain, the service skips pending-disposal resolution rather
// than silently picking a wrong asset_id and paying out proceeds against it.
// The lot itself must still be priced/resolved — that is the user's primary
// intent.
func TestSetManualPrice_AmbiguousSymbol_SkipsDisposalResolution(t *testing.T) {
	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	userID := uuid.New()
	walletID := uuid.New()
	accountID := uuid.New()
	lotID := uuid.New()

	lotRepo := &fakeLotRepo{
		lot: &ledger.TaxLot{
			ID:                lotID,
			TransactionID:     uuid.New(),
			AccountID:         accountID,
			Asset:             "USDT", // intentionally multi-chain symbol
			QuantityAcquired:  big.NewInt(1_000),
			QuantityRemaining: big.NewInt(1_000),
			AcquiredAt:        time.Now().UTC().Truncate(time.Minute),
			PriceStatus:       ledger.PriceStatusPending,
			// ChainID left empty — simulates today's reality where
			// GetTaxLotForUpdate does not populate it.
		},
	}
	ledgerRepo := &fakeLedgerRepo{
		account: &ledger.Account{ID: accountID, WalletID: &walletID},
	}
	walletRepo := &fakeWalletRepo{wallet: &wallet.Wallet{ID: walletID, UserID: userID}}

	ethChain := "ethereum"
	bnbChain := "binance-smart-chain"
	assetLookup := &fakeAssetLookup{
		multi: []asset.Asset{
			{ID: uuid.New(), Symbol: "USDT", ChainID: &ethChain},
			{ID: uuid.New(), Symbol: "USDT", ChainID: &bnbChain},
		},
	}

	svc := lots.NewService(lotRepo, ledgerRepo, walletRepo).WithAssetLookup(assetLookup, log)

	err := svc.SetManualPrice(ctx, userID, lotID, "1.00", "manual price")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Lot-side resolution still happened (primary intent).
	if !lotRepo.markResolvedCalled {
		t.Error("expected MarkResolved to be called despite ambiguous symbol")
	}

	// Plural lookup consulted since ChainID is empty.
	if len(assetLookup.pluralCalls) != 1 {
		t.Fatalf("expected 1 GetAssetsBySymbol call, got %d", len(assetLookup.pluralCalls))
	}
	if assetLookup.pluralCalls[0] != "USDT" {
		t.Errorf("expected GetAssetsBySymbol with USDT, got %s", assetLookup.pluralCalls[0])
	}

	// Disposal resolution must NOT have been called — we can't pick the right asset.
	if len(lotRepo.resolveDispCalls) != 0 {
		t.Errorf("expected 0 ResolvePendingDisposalsForUser calls on ambiguous symbol, got %d",
			len(lotRepo.resolveDispCalls))
	}
}

// TestSetManualPrice_NoChainHint_UniqueSymbolResolves verifies the fallback:
// when the lot has no ChainID but the symbol is unique across chains, the
// service uses GetAssetsBySymbol and still resolves the matching pending
// disposals for the user.
func TestSetManualPrice_NoChainHint_UniqueSymbolResolves(t *testing.T) {
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
			ID:                lotID,
			TransactionID:     uuid.New(),
			AccountID:         accountID,
			Asset:             "UNIQ",
			QuantityAcquired:  big.NewInt(500),
			QuantityRemaining: big.NewInt(500),
			AcquiredAt:        acquiredAt,
			PriceStatus:       ledger.PriceStatusPending,
			// ChainID intentionally empty — exercises the plural fallback.
		},
		resolveDispCount: 2,
	}
	ledgerRepo := &fakeLedgerRepo{
		account: &ledger.Account{ID: accountID, WalletID: &walletID},
	}
	walletRepo := &fakeWalletRepo{wallet: &wallet.Wallet{ID: walletID, UserID: userID}}

	ethChain := "ethereum"
	assetLookup := &fakeAssetLookup{
		multi: []asset.Asset{{ID: assetID, Symbol: "UNIQ", ChainID: &ethChain}},
	}

	svc := lots.NewService(lotRepo, ledgerRepo, walletRepo).WithAssetLookup(assetLookup, log)

	err := svc.SetManualPrice(ctx, userID, lotID, "2.50", "manual price")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(assetLookup.pluralCalls) != 1 {
		t.Fatalf("expected 1 GetAssetsBySymbol call, got %d", len(assetLookup.pluralCalls))
	}
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
	// 2.50 USD scaled 10^8 = 250_000_000
	if call.proceeds.String() != "250000000" {
		t.Errorf("expected proceeds 250000000, got %s", call.proceeds.String())
	}
}
