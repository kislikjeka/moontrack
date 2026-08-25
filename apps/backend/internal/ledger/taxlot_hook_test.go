package ledger

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// mockLedgerRepo implements a minimal ledger.Repository for hook tests.
// Only GetAccount is needed; other methods panic if called unexpectedly.
type mockLedgerRepo struct {
	accounts map[uuid.UUID]*Account
}

func (m *mockLedgerRepo) GetAccount(_ context.Context, id uuid.UUID) (*Account, error) {
	if a, ok := m.accounts[id]; ok {
		return a, nil
	}
	return nil, ErrInvalidAccountCode
}

// Unused methods — satisfy interface
func (m *mockLedgerRepo) CreateAccount(context.Context, *Account) error { return nil }
func (m *mockLedgerRepo) GetOrCreateAccount(_ context.Context, a *Account) (*Account, error) {
	return a, nil
}
func (m *mockLedgerRepo) GetAccountByCode(context.Context, string) (*Account, error) { return nil, nil }
func (m *mockLedgerRepo) FindAccountsByWallet(context.Context, uuid.UUID) ([]*Account, error) {
	return nil, nil
}
func (m *mockLedgerRepo) CreateTransaction(context.Context, *Transaction) error { return nil }
func (m *mockLedgerRepo) GetTransaction(context.Context, uuid.UUID) (*Transaction, error) {
	return nil, nil
}
func (m *mockLedgerRepo) FindTransactionsBySource(context.Context, string, string) (*Transaction, error) {
	return nil, nil
}
func (m *mockLedgerRepo) ListTransactions(context.Context, TransactionFilters) ([]*Transaction, error) {
	return nil, nil
}
func (m *mockLedgerRepo) GetEntriesByTransaction(context.Context, uuid.UUID) ([]*Entry, error) {
	return nil, nil
}
func (m *mockLedgerRepo) GetEntriesByAccount(context.Context, uuid.UUID) ([]*Entry, error) {
	return nil, nil
}
func (m *mockLedgerRepo) GetAccountBalance(_ context.Context, id uuid.UUID, asset uuid.UUID) (*AccountBalance, error) {
	return &AccountBalance{AccountID: id, AssetID: asset, Balance: big.NewInt(0), USDValue: big.NewInt(0), LastUpdated: time.Now()}, nil
}
func (m *mockLedgerRepo) GetAccountBalanceForUpdate(_ context.Context, id uuid.UUID, asset uuid.UUID) (*AccountBalance, error) {
	return &AccountBalance{AccountID: id, AssetID: asset, Balance: big.NewInt(0), USDValue: big.NewInt(0), LastUpdated: time.Now()}, nil
}
func (m *mockLedgerRepo) UpsertAccountBalance(context.Context, *AccountBalance) error { return nil }
func (m *mockLedgerRepo) GetAccountBalances(context.Context, uuid.UUID) ([]*AccountBalance, error) {
	return nil, nil
}
func (m *mockLedgerRepo) CalculateBalanceFromEntries(context.Context, uuid.UUID, uuid.UUID) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (m *mockLedgerRepo) BeginTx(ctx context.Context) (context.Context, error) { return ctx, nil }
func (m *mockLedgerRepo) CommitTx(context.Context) error                       { return nil }
func (m *mockLedgerRepo) RollbackTx(context.Context) error                     { return nil }

// test helpers

func newTestLogger() *logger.Logger {
	return logger.NewDefault("test")
}

func walletAccount(id uuid.UUID) *Account {
	wid := uuid.New()
	return &Account{
		ID:       id,
		Code:     "wallet.test.eth.ETH",
		Type:     AccountTypeCryptoWallet,
		AssetID:  testasset.ETH,
		WalletID: &wid,
	}
}

func incomeAccount(id uuid.UUID) *Account {
	return &Account{
		ID:      id,
		Code:    "income.eth.ETH",
		Type:    AccountTypeIncome,
		AssetID: testasset.ETH,
	}
}

func expenseAccount(id uuid.UUID) *Account {
	return &Account{
		ID:      id,
		Code:    "expense.eth.ETH",
		Type:    AccountTypeExpense,
		AssetID: testasset.ETH,
	}
}

func gasAccount(id uuid.UUID) *Account {
	return &Account{
		ID:      id,
		Code:    "gas.eth.ETH",
		Type:    AccountTypeGasFee,
		AssetID: testasset.ETH,
	}
}

func makeEntry(accountID uuid.UUID, dc DebitCredit, et EntryType, amount int64, asset uuid.UUID, meta map[string]interface{}) *Entry {
	return &Entry{
		ID:          uuid.New(),
		AccountID:   accountID,
		DebitCredit: dc,
		EntryType:   et,
		Amount:      big.NewInt(amount),
		AssetID:     asset,
		USDRate:     big.NewInt(200_000_000_00), // $200 scaled 10^8
		USDValue:    big.NewInt(0),
		OccurredAt:  time.Now(),
		CreatedAt:   time.Now(),
		Metadata:    meta,
	}
}

// paired stamps a leg-pair marker onto entry metadata, which is what the hook
// now follows to carry a cost basis from a disposal to its acquisition. Both
// legs of one movement get the SAME marker; gas and anything else that is not
// part of a pair gets none. See [MetaLegPair].
func paired(pair string, meta map[string]interface{}) map[string]interface{} {
	m := map[string]interface{}{}
	for k, v := range meta {
		m[k] = v
	}
	m[MetaLegPair] = pair
	return m
}

// --- Tests ---

func TestTaxLotHook_TransferIn_CreatesLot(t *testing.T) {
	walletAcctID := uuid.New()
	incomeAcctID := uuid.New()

	taxLotRepo := &mockTaxLotRepo{}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID: walletAccount(walletAcctID),
		incomeAcctID: incomeAccount(incomeAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeTransferIn,
		Entries: []*Entry{
			makeEntry(walletAcctID, Debit, EntryTypeAssetIncrease, 1000, testasset.ETH, nil),
			makeEntry(incomeAcctID, Credit, EntryTypeIncome, 1000, testasset.ETH, nil),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.lots) != 1 {
		t.Fatalf("expected 1 lot, got %d", len(taxLotRepo.lots))
	}
	lot := taxLotRepo.lots[0]
	if lot.AutoCostBasisSource != CostBasisFMVAtTransfer {
		t.Errorf("expected source fmv_at_transfer, got %s", lot.AutoCostBasisSource)
	}
	if lot.QuantityAcquired.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("expected quantity 1000, got %s", lot.QuantityAcquired)
	}
	if len(taxLotRepo.disposals) != 0 {
		t.Errorf("expected 0 disposals, got %d", len(taxLotRepo.disposals))
	}
}

func TestTaxLotHook_TransferOut_DisposesLot(t *testing.T) {
	walletAcctID := uuid.New()
	expenseAcctID := uuid.New()

	// Pre-seed a lot
	existingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            walletAcctID,
		Asset:                testasset.ETH,
		QuantityAcquired:     big.NewInt(1000),
		QuantityRemaining:    big.NewInt(1000),
		AcquiredAt:           time.Now().Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(100_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		CreatedAt:            time.Now(),
	}

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{existingLot}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID:  walletAccount(walletAcctID),
		expenseAcctID: expenseAccount(expenseAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeTransferOut,
		Entries: []*Entry{
			makeEntry(expenseAcctID, Debit, EntryTypeExpense, 500, testasset.ETH, nil),
			makeEntry(walletAcctID, Credit, EntryTypeAssetDecrease, 500, testasset.ETH, nil),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.disposals) != 1 {
		t.Fatalf("expected 1 disposal, got %d", len(taxLotRepo.disposals))
	}
	if taxLotRepo.disposals[0].DisposalType != DisposalTypeSale {
		t.Errorf("expected disposal type sale, got %s", taxLotRepo.disposals[0].DisposalType)
	}
	if existingLot.QuantityRemaining.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("expected remaining 500, got %s", existingLot.QuantityRemaining)
	}
	// No new lots should be created
	if len(taxLotRepo.lots) != 1 {
		t.Errorf("expected 1 lot (existing), got %d", len(taxLotRepo.lots))
	}
}

func TestTaxLotHook_TransferOutWithGas_TwoDisposals(t *testing.T) {
	walletAcctID := uuid.New()
	expenseAcctID := uuid.New()
	gasAcctID := uuid.New()

	// Pre-seed lots for ETH (both main asset and gas token are ETH here)
	existingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            walletAcctID,
		Asset:                testasset.ETH,
		QuantityAcquired:     big.NewInt(10000),
		QuantityRemaining:    big.NewInt(10000),
		AcquiredAt:           time.Now().Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(100_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		CreatedAt:            time.Now(),
	}

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{existingLot}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID:  walletAccount(walletAcctID),
		expenseAcctID: expenseAccount(expenseAcctID),
		gasAcctID:     gasAccount(gasAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeTransferOut,
		Entries: []*Entry{
			makeEntry(expenseAcctID, Debit, EntryTypeExpense, 500, testasset.ETH, nil),
			makeEntry(walletAcctID, Credit, EntryTypeAssetDecrease, 500, testasset.ETH, nil),
			makeEntry(gasAcctID, Debit, EntryTypeGasFee, 10, testasset.ETH, nil),
			makeEntry(walletAcctID, Credit, EntryTypeAssetDecrease, 10, testasset.ETH,
				map[string]interface{}{"entry_type": "gas_payment"}),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.disposals) != 2 {
		t.Fatalf("expected 2 disposals, got %d", len(taxLotRepo.disposals))
	}

	// First disposal is the main asset decrease (sale)
	if taxLotRepo.disposals[0].DisposalType != DisposalTypeSale {
		t.Errorf("expected first disposal type sale, got %s", taxLotRepo.disposals[0].DisposalType)
	}
	// Second disposal is gas
	if taxLotRepo.disposals[1].DisposalType != DisposalTypeGasFee {
		t.Errorf("expected second disposal type gas_fee, got %s", taxLotRepo.disposals[1].DisposalType)
	}
}

func TestTaxLotHook_Swap_DisposalPlusAcquisition(t *testing.T) {
	walletAcctID := uuid.New()
	clearingAcctID := uuid.New()

	// Pre-seed lot for asset being sold
	existingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            walletAcctID,
		Asset:                testasset.USDC,
		QuantityAcquired:     big.NewInt(5000),
		QuantityRemaining:    big.NewInt(5000),
		AcquiredAt:           time.Now().Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(100_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		CreatedAt:            time.Now(),
	}

	walletAcct := walletAccount(walletAcctID)

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{existingLot}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID:   walletAcct,
		clearingAcctID: {ID: clearingAcctID, Code: "clearing.swap", Type: AccountTypeClearing, AssetID: testasset.USDC},
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeSwap,
		Entries: []*Entry{
			// Sold USDC
			makeEntry(walletAcctID, Credit, EntryTypeAssetDecrease, 2000, testasset.USDC, nil),
			makeEntry(clearingAcctID, Debit, EntryTypeClearing, 2000, testasset.USDC, nil),
			// Bought ETH
			makeEntry(walletAcctID, Debit, EntryTypeAssetIncrease, 100, testasset.ETH, nil),
			makeEntry(clearingAcctID, Credit, EntryTypeClearing, 100, testasset.ETH, nil),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 disposal (USDC sold)
	if len(taxLotRepo.disposals) != 1 {
		t.Fatalf("expected 1 disposal, got %d", len(taxLotRepo.disposals))
	}
	if taxLotRepo.disposals[0].DisposalType != DisposalTypeSale {
		t.Errorf("expected disposal type sale, got %s", taxLotRepo.disposals[0].DisposalType)
	}

	// Should have 1 new lot (ETH bought) + the existing USDC lot
	if len(taxLotRepo.lots) != 2 {
		t.Fatalf("expected 2 lots (1 existing + 1 new), got %d", len(taxLotRepo.lots))
	}
	newLot := taxLotRepo.lots[1]
	if newLot.Asset != testasset.ETH {
		t.Errorf("expected new lot asset ETH, got %s", newLot.Asset)
	}
	if newLot.AutoCostBasisSource != CostBasisSwapPrice {
		t.Errorf("expected source swap_price, got %s", newLot.AutoCostBasisSource)
	}
}

func TestTaxLotHook_InternalTransfer_DisposalPlusLinkedLot(t *testing.T) {
	srcWalletAcctID := uuid.New()
	dstWalletAcctID := uuid.New()

	// Pre-seed lot on source wallet
	existingLot := &TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            srcWalletAcctID,
		Asset:                testasset.ETH,
		QuantityAcquired:     big.NewInt(1000),
		QuantityRemaining:    big.NewInt(1000),
		AcquiredAt:           time.Now().Add(-time.Hour),
		AutoCostBasisPerUnit: big.NewInt(200_000_000),
		AutoCostBasisSource:  CostBasisFMVAtTransfer,
		CreatedAt:            time.Now(),
	}

	srcAcct := walletAccount(srcWalletAcctID)
	dstAcct := walletAccount(dstWalletAcctID)

	taxLotRepo := &mockTaxLotRepo{lots: []*TaxLot{existingLot}}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		srcWalletAcctID: srcAcct,
		dstWalletAcctID: dstAcct,
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeInternalTransfer,
		Entries: []*Entry{
			makeEntry(dstWalletAcctID, Debit, EntryTypeAssetIncrease, 500, testasset.ETH, paired("move", nil)),
			makeEntry(srcWalletAcctID, Credit, EntryTypeAssetDecrease, 500, testasset.ETH, paired("move", nil)),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Disposal on source
	if len(taxLotRepo.disposals) != 1 {
		t.Fatalf("expected 1 disposal, got %d", len(taxLotRepo.disposals))
	}
	if taxLotRepo.disposals[0].DisposalType != DisposalTypeInternalTransfer {
		t.Errorf("expected disposal type internal_transfer, got %s", taxLotRepo.disposals[0].DisposalType)
	}

	// New lot on destination
	if len(taxLotRepo.lots) != 2 {
		t.Fatalf("expected 2 lots, got %d", len(taxLotRepo.lots))
	}
	newLot := taxLotRepo.lots[1]
	if newLot.AutoCostBasisSource != CostBasisLinkedTransfer {
		t.Errorf("expected source linked_transfer, got %s", newLot.AutoCostBasisSource)
	}
	if newLot.LinkedSourceLotID == nil {
		t.Error("expected LinkedSourceLotID to be set")
	} else if *newLot.LinkedSourceLotID != existingLot.ID {
		t.Errorf("expected linked lot %s, got %s", existingLot.ID, *newLot.LinkedSourceLotID)
	}

	// Cost basis should carry over from source lot (200_000_000 = $2/unit),
	// NOT use FMV at transfer time (200_000_000_00 = $200/unit from entry USDRate).
	expectedCostBasis := big.NewInt(200_000_000) // matches existingLot.AutoCostBasisPerUnit
	if newLot.AutoCostBasisPerUnit.Cmp(expectedCostBasis) != 0 {
		t.Errorf("expected cost basis carry-over %s, got %s",
			expectedCostBasis, newLot.AutoCostBasisPerUnit)
	}
}

func TestTaxLotHook_NonWalletEntries_Skipped(t *testing.T) {
	incomeAcctID := uuid.New()
	expenseAcctID := uuid.New()

	taxLotRepo := &mockTaxLotRepo{}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		incomeAcctID:  incomeAccount(incomeAcctID),
		expenseAcctID: expenseAccount(expenseAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeManualIncome,
		Entries: []*Entry{
			makeEntry(incomeAcctID, Credit, EntryTypeIncome, 500, testasset.ETH, nil),
			makeEntry(expenseAcctID, Debit, EntryTypeExpense, 500, testasset.ETH, nil),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.lots) != 0 {
		t.Errorf("expected 0 lots, got %d", len(taxLotRepo.lots))
	}
	if len(taxLotRepo.disposals) != 0 {
		t.Errorf("expected 0 disposals, got %d", len(taxLotRepo.disposals))
	}
}

func TestTaxLotHook_InsufficientLots_WarnsButSucceeds(t *testing.T) {
	walletAcctID := uuid.New()
	expenseAcctID := uuid.New()

	// No pre-existing lots
	taxLotRepo := &mockTaxLotRepo{}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{
		walletAcctID:  walletAccount(walletAcctID),
		expenseAcctID: expenseAccount(expenseAcctID),
	}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:   uuid.New(),
		Type: TxTypeTransferOut,
		Entries: []*Entry{
			makeEntry(expenseAcctID, Debit, EntryTypeExpense, 500, testasset.ETH, nil),
			makeEntry(walletAcctID, Credit, EntryTypeAssetDecrease, 500, testasset.ETH, nil),
		},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("expected hook to succeed despite insufficient lots, got: %v", err)
	}
}

func TestTaxLotHook_EmptyTransaction_NoOp(t *testing.T) {
	taxLotRepo := &mockTaxLotRepo{}
	ledgerRepo := &mockLedgerRepo{accounts: map[uuid.UUID]*Account{}}

	hook := NewTaxLotHook(taxLotRepo, ledgerRepo, newTestLogger())

	tx := &Transaction{
		ID:      uuid.New(),
		Type:    TxTypeTransferIn,
		Entries: []*Entry{},
	}

	err := hook(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(taxLotRepo.lots) != 0 {
		t.Errorf("expected 0 lots, got %d", len(taxLotRepo.lots))
	}
}
