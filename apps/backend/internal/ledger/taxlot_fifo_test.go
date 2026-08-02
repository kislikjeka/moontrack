package ledger

import (
	"context"
	"math/big"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// mockTaxLotRepo is a simple in-memory mock of TaxLotRepository for FIFO tests.
//
// The `mu` mutex guards all reads and writes so the mock is safe to use from
// tests that exercise concurrency (e.g. PriceResolvedHook contention).
type mockTaxLotRepo struct {
	mu        sync.Mutex
	lots      []*TaxLot
	disposals []*LotDisposal

	// lotAssetIDs associates a disposal's lot with the asset UUID that owns it.
	//
	// Lots no longer need this — TaxLot.Asset is itself the registry UUID (#59),
	// so ListPendingLotsByAssetAndTime matches on the lot directly. Disposals
	// still do: LotDisposal carries no asset of its own, and in production the
	// scoping comes from a JOIN back to the lot, which this in-memory mock has
	// to stand in for.
	lotAssetIDs map[uuid.UUID]uuid.UUID

	// failResolveOn lets tests inject a deterministic error on the Nth
	// call (1-indexed) to ResolvePendingPrice. Zero means never fail.
	failResolveOn   int
	resolveCalls    int
	failResolveErr  error
	resolveCallback func(lotID uuid.UUID)
}

func (m *mockTaxLotRepo) CreateTaxLot(_ context.Context, lot *TaxLot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lots = append(m.lots, lot)
	return nil
}

func (m *mockTaxLotRepo) GetTaxLot(_ context.Context, id uuid.UUID) (*TaxLot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.lots {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, ErrLotNotFound
}

func (m *mockTaxLotRepo) GetTaxLotForUpdate(_ context.Context, id uuid.UUID) (*TaxLot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.lots {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, ErrLotNotFound
}

func (m *mockTaxLotRepo) GetOpenLotsFIFO(_ context.Context, accountID uuid.UUID, asset uuid.UUID) ([]*TaxLot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var open []*TaxLot
	for _, l := range m.lots {
		if l.AccountID == accountID && l.Asset == asset && l.IsOpen() {
			open = append(open, l)
		}
	}
	sort.Slice(open, func(i, j int) bool {
		return open[i].AcquiredAt.Before(open[j].AcquiredAt)
	})
	return open, nil
}

func (m *mockTaxLotRepo) UpdateLotRemaining(_ context.Context, lotID uuid.UUID, newRemaining *big.Int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.lots {
		if l.ID == lotID {
			l.QuantityRemaining = new(big.Int).Set(newRemaining)
			return nil
		}
	}
	return ErrLotNotFound
}

func (m *mockTaxLotRepo) GetLotsByAccount(_ context.Context, _ uuid.UUID, _ uuid.UUID) ([]*TaxLot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil, nil
}

func (m *mockTaxLotRepo) GetLotsByTransaction(_ context.Context, txID uuid.UUID) ([]*TaxLot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*TaxLot
	for _, l := range m.lots {
		if l.TransactionID == txID {
			result = append(result, l)
		}
	}
	return result, nil
}

func (m *mockTaxLotRepo) CreateDisposal(_ context.Context, disposal *LotDisposal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disposals = append(m.disposals, disposal)
	return nil
}

func (m *mockTaxLotRepo) GetDisposalsByTransaction(_ context.Context, _ uuid.UUID) ([]*LotDisposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil, nil
}

func (m *mockTaxLotRepo) GetDisposalsByLot(_ context.Context, _ uuid.UUID) ([]*LotDisposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil, nil
}

func (m *mockTaxLotRepo) UpdateOverride(_ context.Context, _ uuid.UUID, _ *big.Int, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockTaxLotRepo) ClearOverride(_ context.Context, _ uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockTaxLotRepo) CreateOverrideHistory(_ context.Context, _ *LotOverrideHistory) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockTaxLotRepo) GetOverrideHistory(_ context.Context, _ uuid.UUID) ([]*LotOverrideHistory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil, nil
}

func (m *mockTaxLotRepo) RefreshWAC(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockTaxLotRepo) GetWAC(_ context.Context, _ []uuid.UUID) ([]*PositionWAC, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil, nil
}

// ListPendingLotsByAssetAndTime is the single survivor of what used to be a
// pair (#59): a symbol-keyed variant and a UUID-keyed one. The UUID-keyed mock
// needed a side map (lotAssetIDs) plus a permissive "no mapping registered ⇒
// match everything in the bucket" fallback, because a lot's own Asset field was
// a bare ticker and could not answer which asset the lot belonged to.
//
// TaxLot.Asset is now the registry UUID, so the match is direct and exact. The
// fallback is gone with it — a lot in the right minute bucket but the wrong
// asset is no longer returned, which is the whole point of the ticket.
func (m *mockTaxLotRepo) ListPendingLotsByAssetAndTime(_ context.Context, assetID uuid.UUID, at time.Time) ([]*TaxLot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*TaxLot
	for _, l := range m.lots {
		if l.Asset == assetID && l.PriceStatus == PriceStatusPending &&
			l.AcquiredAt.Truncate(time.Minute).Equal(at.Truncate(time.Minute)) {
			result = append(result, l)
		}
	}
	return result, nil
}

func (m *mockTaxLotRepo) ResolvePendingPrice(_ context.Context, lotID uuid.UUID, autoCostBasisPerUnit *big.Int, autoSource CostBasisSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolveCalls++
	if m.failResolveOn > 0 && m.resolveCalls == m.failResolveOn {
		err := m.failResolveErr
		if err == nil {
			err = ErrLotNotFound
		}
		return err
	}
	if m.resolveCallback != nil {
		m.resolveCallback(lotID)
	}
	for _, l := range m.lots {
		if l.ID == lotID && l.PriceStatus == PriceStatusPending {
			l.AutoCostBasisPerUnit = new(big.Int).Set(autoCostBasisPerUnit)
			l.AutoCostBasisSource = autoSource
			l.PriceStatus = PriceStatusResolved
			return nil
		}
	}
	return ErrLotNotFound
}

func (m *mockTaxLotRepo) ResolvePendingDisposals(_ context.Context, assetID uuid.UUID, at time.Time, proceeds *big.Int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var updated int64
	// Build a lotID -> asset UUID map if tests registered one; fall back
	// to permissive matching when no mapping is registered.
	for _, d := range m.disposals {
		if d.ProceedsStatus != ProceedsStatusPending {
			continue
		}
		// Scope by minute bucket.
		if !d.DisposedAt.Truncate(time.Minute).Equal(at.Truncate(time.Minute)) {
			continue
		}
		owner, ok := m.lotAssetIDs[d.LotID]
		if ok && owner != assetID {
			continue
		}
		d.ProceedsPerUnit = new(big.Int).Set(proceeds)
		d.ProceedsStatus = ProceedsStatusResolved
		updated++
	}
	return updated, nil
}

// ResolvePendingDisposalsForUser mirrors ResolvePendingDisposals for the
// mock. The in-memory model has no user/wallet ownership map, so the mock
// simply delegates to the global variant. Tests that care about tenant
// scoping live under internal/infra/postgres where the SQL predicate can
// actually be exercised end-to-end.
func (m *mockTaxLotRepo) ResolvePendingDisposalsForUser(ctx context.Context, _ uuid.UUID, assetID uuid.UUID, at time.Time, proceeds *big.Int) (int64, error) {
	return m.ResolvePendingDisposals(ctx, assetID, at, proceeds)
}

func (m *mockTaxLotRepo) MarkUnpriceable(_ context.Context, _ uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockTaxLotRepo) MarkResolved(_ context.Context, _ uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockTaxLotRepo) IncrementAttempt(_ context.Context, _ uuid.UUID, _ int, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return nil
}

func (m *mockTaxLotRepo) CountLotsByPriceStatus(_ context.Context, _ uuid.UUID) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return 0, 0, nil
}

// helpers

func makeLot(accountID uuid.UUID, asset uuid.UUID, qty int64, acquiredAt time.Time) *TaxLot {
	return &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            accountID,
		Asset:                asset,
		QuantityAcquired:     big.NewInt(qty),
		QuantityRemaining:    big.NewInt(qty),
		AcquiredAt:           acquiredAt,
		AutoCostBasisPerUnit: big.NewInt(100_000_000), // $1.00 scaled 10^8
		AutoCostBasisSource:  CostBasisSwapPrice,
		CreatedAt:            time.Now(),
	}
}

func bigInt(n int64) *big.Int {
	return big.NewInt(n)
}

// tests

func TestDisposeFIFO_SingleLotExact(t *testing.T) {
	accountID := uuid.New()
	txID := uuid.New()
	now := time.Now()
	asset := testasset.ETH

	lot := makeLot(accountID, asset, 100, now.Add(-time.Hour))
	repo := &mockTaxLotRepo{lots: []*TaxLot{lot}}

	disposals, err := DisposeFIFO(
		context.Background(), repo,
		accountID, asset,
		bigInt(100), bigInt(200_000_000),
		DisposalTypeSale, txID, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(disposals) != 1 {
		t.Fatalf("expected 1 disposal, got %d", len(disposals))
	}
	if disposals[0].QuantityDisposed.Cmp(bigInt(100)) != 0 {
		t.Errorf("expected disposal qty 100, got %s", disposals[0].QuantityDisposed)
	}
	if lot.QuantityRemaining.Sign() != 0 {
		t.Errorf("expected lot remaining 0, got %s", lot.QuantityRemaining)
	}
}

func TestDisposeFIFO_SingleLotPartial(t *testing.T) {
	accountID := uuid.New()
	txID := uuid.New()
	now := time.Now()
	asset := testasset.ETH

	lot := makeLot(accountID, asset, 100, now.Add(-time.Hour))
	repo := &mockTaxLotRepo{lots: []*TaxLot{lot}}

	disposals, err := DisposeFIFO(
		context.Background(), repo,
		accountID, asset,
		bigInt(60), bigInt(200_000_000),
		DisposalTypeSale, txID, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(disposals) != 1 {
		t.Fatalf("expected 1 disposal, got %d", len(disposals))
	}
	if disposals[0].QuantityDisposed.Cmp(bigInt(60)) != 0 {
		t.Errorf("expected disposal qty 60, got %s", disposals[0].QuantityDisposed)
	}
	if lot.QuantityRemaining.Cmp(bigInt(40)) != 0 {
		t.Errorf("expected lot remaining 40, got %s", lot.QuantityRemaining)
	}
}

func TestDisposeFIFO_MultiLotFIFOOrdering(t *testing.T) {
	accountID := uuid.New()
	txID := uuid.New()
	now := time.Now()
	asset := testasset.ETH

	jan := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2025, 2, 15, 0, 0, 0, 0, time.UTC)

	lotA := makeLot(accountID, asset, 50, jan)
	lotB := makeLot(accountID, asset, 80, feb)
	// Insert in reverse order to verify sorting
	repo := &mockTaxLotRepo{lots: []*TaxLot{lotB, lotA}}

	disposals, err := DisposeFIFO(
		context.Background(), repo,
		accountID, asset,
		bigInt(70), bigInt(300_000_000),
		DisposalTypeSale, txID, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(disposals) != 2 {
		t.Fatalf("expected 2 disposals, got %d", len(disposals))
	}

	// First disposal should be from lotA (January - oldest)
	if disposals[0].LotID != lotA.ID {
		t.Errorf("expected first disposal from lotA, got lot %s", disposals[0].LotID)
	}
	if disposals[0].QuantityDisposed.Cmp(bigInt(50)) != 0 {
		t.Errorf("expected first disposal qty 50, got %s", disposals[0].QuantityDisposed)
	}

	// Second disposal should be from lotB (February)
	if disposals[1].LotID != lotB.ID {
		t.Errorf("expected second disposal from lotB, got lot %s", disposals[1].LotID)
	}
	if disposals[1].QuantityDisposed.Cmp(bigInt(20)) != 0 {
		t.Errorf("expected second disposal qty 20, got %s", disposals[1].QuantityDisposed)
	}

	if lotA.QuantityRemaining.Sign() != 0 {
		t.Errorf("expected lotA remaining 0, got %s", lotA.QuantityRemaining)
	}
	if lotB.QuantityRemaining.Cmp(bigInt(60)) != 0 {
		t.Errorf("expected lotB remaining 60, got %s", lotB.QuantityRemaining)
	}
}

func TestDisposeFIFO_MultiLotFullConsumption(t *testing.T) {
	accountID := uuid.New()
	txID := uuid.New()
	now := time.Now()
	asset := testasset.ETH

	lotA := makeLot(accountID, asset, 50, now.Add(-2*time.Hour))
	lotB := makeLot(accountID, asset, 80, now.Add(-time.Hour))
	repo := &mockTaxLotRepo{lots: []*TaxLot{lotA, lotB}}

	disposals, err := DisposeFIFO(
		context.Background(), repo,
		accountID, asset,
		bigInt(130), bigInt(200_000_000),
		DisposalTypeSale, txID, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(disposals) != 2 {
		t.Fatalf("expected 2 disposals, got %d", len(disposals))
	}
	if lotA.QuantityRemaining.Sign() != 0 {
		t.Errorf("expected lotA remaining 0, got %s", lotA.QuantityRemaining)
	}
	if lotB.QuantityRemaining.Sign() != 0 {
		t.Errorf("expected lotB remaining 0, got %s", lotB.QuantityRemaining)
	}
}

func TestDisposeFIFO_InsufficientLots(t *testing.T) {
	accountID := uuid.New()
	txID := uuid.New()
	now := time.Now()
	asset := testasset.ETH

	lotA := makeLot(accountID, asset, 50, now.Add(-time.Hour))
	repo := &mockTaxLotRepo{lots: []*TaxLot{lotA}}

	disposals, err := DisposeFIFO(
		context.Background(), repo,
		accountID, asset,
		bigInt(100), bigInt(200_000_000),
		DisposalTypeSale, txID, now,
	)
	if err != ErrInsufficientLots {
		t.Fatalf("expected ErrInsufficientLots, got %v", err)
	}
	// Partial disposal should still be created
	if len(disposals) != 1 {
		t.Fatalf("expected 1 partial disposal, got %d", len(disposals))
	}
	if disposals[0].QuantityDisposed.Cmp(bigInt(50)) != 0 {
		t.Errorf("expected disposal qty 50, got %s", disposals[0].QuantityDisposed)
	}
	if lotA.QuantityRemaining.Sign() != 0 {
		t.Errorf("expected lotA remaining 0, got %s", lotA.QuantityRemaining)
	}
}

func TestDisposeFIFO_ZeroLotsAvailable(t *testing.T) {
	accountID := uuid.New()
	txID := uuid.New()
	now := time.Now()
	asset := testasset.ETH

	repo := &mockTaxLotRepo{}

	disposals, err := DisposeFIFO(
		context.Background(), repo,
		accountID, asset,
		bigInt(100), bigInt(200_000_000),
		DisposalTypeSale, txID, now,
	)
	if err != ErrInsufficientLots {
		t.Fatalf("expected ErrInsufficientLots, got %v", err)
	}
	if len(disposals) != 0 {
		t.Fatalf("expected 0 disposals, got %d", len(disposals))
	}
}

func TestDisposeFIFO_ZeroQuantity(t *testing.T) {
	accountID := uuid.New()
	txID := uuid.New()
	now := time.Now()
	asset := testasset.ETH

	lot := makeLot(accountID, asset, 100, now.Add(-time.Hour))
	repo := &mockTaxLotRepo{lots: []*TaxLot{lot}}

	// Zero quantity
	disposals, err := DisposeFIFO(
		context.Background(), repo,
		accountID, asset,
		bigInt(0), bigInt(200_000_000),
		DisposalTypeSale, txID, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(disposals) != 0 {
		t.Fatalf("expected 0 disposals for zero quantity, got %d", len(disposals))
	}

	// Nil quantity
	disposals, err = DisposeFIFO(
		context.Background(), repo,
		accountID, asset,
		nil, bigInt(200_000_000),
		DisposalTypeSale, txID, now,
	)
	if err != nil {
		t.Fatalf("unexpected error for nil quantity: %v", err)
	}
	if len(disposals) != 0 {
		t.Fatalf("expected 0 disposals for nil quantity, got %d", len(disposals))
	}

	// Lot should be untouched
	if lot.QuantityRemaining.Cmp(bigInt(100)) != 0 {
		t.Errorf("expected lot remaining unchanged at 100, got %s", lot.QuantityRemaining)
	}
}
