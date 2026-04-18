package ledger

import (
	"context"
	"math/big"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockTaxLotRepo is a simple in-memory mock of TaxLotRepository for FIFO tests.
//
// The `mu` mutex guards all reads and writes so the mock is safe to use from
// tests that exercise concurrency (e.g. PriceResolvedHook contention).
type mockTaxLotRepo struct {
	mu        sync.Mutex
	lots      []*TaxLot
	disposals []*LotDisposal

	// lotAssetIDs optionally associates a lot with a concrete asset UUID.
	// Used by PriceResolvedHook tests that exercise the UUID-keyed variant.
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

func (m *mockTaxLotRepo) GetOpenLotsFIFO(_ context.Context, accountID uuid.UUID, asset string) ([]*TaxLot, error) {
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

func (m *mockTaxLotRepo) GetLotsByAccount(_ context.Context, _ uuid.UUID, _ string) ([]*TaxLot, error) {
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

func (m *mockTaxLotRepo) ListPendingLotsByAssetAndTime(_ context.Context, asset string, at time.Time) ([]*TaxLot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*TaxLot
	for _, l := range m.lots {
		if l.Asset == asset && l.PriceStatus == PriceStatusPending &&
			l.AcquiredAt.Truncate(time.Minute).Equal(at.Truncate(time.Minute)) {
			result = append(result, l)
		}
	}
	return result, nil
}

// mockAssetID is set on lots that the mock wants disambiguated by UUID;
// when lot.AccountID's first byte matches a mock marker, treat this as the
// owning asset UUID. Tests drive the mapping directly via lotAssetIDs.
func (m *mockTaxLotRepo) ListPendingLotsByAssetIDAndTime(_ context.Context, assetID uuid.UUID, at time.Time) ([]*TaxLot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*TaxLot
	for _, l := range m.lots {
		owner, ok := m.lotAssetIDs[l.ID]
		// If the test did not register a UUID for this lot, fall back to
		// matching on all pending lots in the bucket (symbol not considered)
		// so existing tests that don't care about UUID still pass.
		if !ok {
			if l.PriceStatus == PriceStatusPending &&
				l.AcquiredAt.Truncate(time.Minute).Equal(at.Truncate(time.Minute)) {
				result = append(result, l)
			}
			continue
		}
		if owner == assetID && l.PriceStatus == PriceStatusPending &&
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

func makeLot(accountID uuid.UUID, asset string, qty int64, acquiredAt time.Time) *TaxLot {
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
	asset := "ETH"

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
	asset := "ETH"

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
	asset := "ETH"

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
	asset := "ETH"

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
	asset := "ETH"

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
	asset := "ETH"

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
	asset := "ETH"

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
