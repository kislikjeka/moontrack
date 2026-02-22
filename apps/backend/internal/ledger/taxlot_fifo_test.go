package ledger

import (
	"context"
	"math/big"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockTaxLotRepo is a simple in-memory mock of TaxLotRepository for FIFO tests.
type mockTaxLotRepo struct {
	lots      []*TaxLot
	disposals []*LotDisposal
}

func (m *mockTaxLotRepo) CreateTaxLot(_ context.Context, lot *TaxLot) error {
	m.lots = append(m.lots, lot)
	return nil
}

func (m *mockTaxLotRepo) GetTaxLot(_ context.Context, id uuid.UUID) (*TaxLot, error) {
	for _, l := range m.lots {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, ErrLotNotFound
}

func (m *mockTaxLotRepo) GetTaxLotForUpdate(_ context.Context, id uuid.UUID) (*TaxLot, error) {
	for _, l := range m.lots {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, ErrLotNotFound
}

func (m *mockTaxLotRepo) GetOpenLotsFIFO(_ context.Context, accountID uuid.UUID, asset string) ([]*TaxLot, error) {
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
	for _, l := range m.lots {
		if l.ID == lotID {
			l.QuantityRemaining = new(big.Int).Set(newRemaining)
			return nil
		}
	}
	return ErrLotNotFound
}

func (m *mockTaxLotRepo) GetLotsByAccount(_ context.Context, _ uuid.UUID, _ string) ([]*TaxLot, error) {
	return nil, nil
}

func (m *mockTaxLotRepo) GetLotsByTransaction(_ context.Context, txID uuid.UUID) ([]*TaxLot, error) {
	var result []*TaxLot
	for _, l := range m.lots {
		if l.TransactionID == txID {
			result = append(result, l)
		}
	}
	return result, nil
}

func (m *mockTaxLotRepo) CreateDisposal(_ context.Context, disposal *LotDisposal) error {
	m.disposals = append(m.disposals, disposal)
	return nil
}

func (m *mockTaxLotRepo) GetDisposalsByTransaction(_ context.Context, _ uuid.UUID) ([]*LotDisposal, error) {
	return nil, nil
}

func (m *mockTaxLotRepo) GetDisposalsByLot(_ context.Context, _ uuid.UUID) ([]*LotDisposal, error) {
	return nil, nil
}

func (m *mockTaxLotRepo) UpdateOverride(_ context.Context, _ uuid.UUID, _ *big.Int, _ string) error {
	return nil
}

func (m *mockTaxLotRepo) ClearOverride(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockTaxLotRepo) CreateOverrideHistory(_ context.Context, _ *LotOverrideHistory) error {
	return nil
}

func (m *mockTaxLotRepo) GetOverrideHistory(_ context.Context, _ uuid.UUID) ([]*LotOverrideHistory, error) {
	return nil, nil
}

func (m *mockTaxLotRepo) RefreshWAC(_ context.Context) error {
	return nil
}

func (m *mockTaxLotRepo) GetWAC(_ context.Context, _ []uuid.UUID) ([]*PositionWAC, error) {
	return nil, nil
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
