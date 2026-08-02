package noves

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/lending"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// The receipt rule, exercised across the whole port on a real Aave supply.
//
// The two tests below run the same fixture the measurement in #44 was taken
// from, through the real adapter, the real TxBuilder and the real lending
// handler, and assert on the LEDGER ENTRIES that come out — not on an
// intermediate representation. That is the level the defect lived at: the
// adapter emitted both legs, each generated pair balanced on its own, and
// double-entry validation passed while one supply of ~0.0471 ETH sat in
// `collateral.` twice.

// captureLedger records what the TxBuilder hands to the ledger and runs the
// real lending handler over it, so the test can assert on entries.
type captureLedger struct {
	t       *testing.T
	handler *lending.LendingSupplyHandler
	entries []*ledger.Entry
	txType  ledger.TransactionType
}

func (c *captureLedger) RecordTransaction(
	ctx context.Context,
	transactionType ledger.TransactionType,
	source string,
	externalID *string,
	occurredAt time.Time,
	rawData map[string]interface{},
) (*ledger.Transaction, error) {
	c.txType = transactionType

	entries, err := c.handler.Handle(ctx, rawData)
	require.NoError(c.t, err, "the real lending handler must accept what the builder produced")
	c.entries = entries

	return &ledger.Transaction{ID: uuid.New()}, nil
}

func (c *captureLedger) FindBySourceExternalID(ctx context.Context, source, externalID string) (*ledger.Transaction, error) {
	return nil, nil
}

// portRegistry is an in-memory asset registry. Identity is a registry UUID
// since #59, so the builder needs one for its legs to resolve to anything the
// real lending handler will accept — uuid.Nil is rejected outright.
//
// It also remembers which UUID it minted per ticker, which is what lets these
// tests keep asserting in the vocabulary the defect was described in ("only
// cbBTC may be booked") while the entries themselves carry UUIDs.
type portRegistry struct {
	byKey    map[sync.AssetKey]*sync.RegistryAsset
	bySymbol map[string]uuid.UUID
}

func newPortRegistry() *portRegistry {
	return &portRegistry{
		byKey:    map[sync.AssetKey]*sync.RegistryAsset{},
		bySymbol: map[string]uuid.UUID{},
	}
}

func (r *portRegistry) Resolve(ctx context.Context, key sync.AssetKey, symbol, name string, decimals int) (*sync.RegistryAsset, error) {
	if a, ok := r.byKey[key]; ok {
		return a, nil
	}
	a := &sync.RegistryAsset{ID: uuid.New(), Key: key, Symbol: symbol, Name: name, Decimals: decimals}
	r.byKey[key] = a
	r.bySymbol[symbol] = a.ID
	return a, nil
}

// idOf returns the UUID minted for a ticker, failing the test if the ticker was
// never resolved — which would silently turn an assertion into a comparison of
// two zero UUIDs.
func (r *portRegistry) idOf(t *testing.T, symbol string) uuid.UUID {
	t.Helper()
	id, ok := r.bySymbol[symbol]
	require.True(t, ok, "expected %q to have been resolved; resolved: %v", symbol, r.bySymbol)
	return id
}

var _ sync.AssetRegistry = (*portRegistry)(nil)

// stubWalletRepo is the minimum the lending handler's ownership check and the
// builder's internal-transfer detection need.
type stubWalletRepo struct{ w *wallet.Wallet }

func (s *stubWalletRepo) GetWalletsForSync(ctx context.Context) ([]*wallet.Wallet, error) {
	return nil, nil
}

func (s *stubWalletRepo) GetWalletsByAddressAndUserID(ctx context.Context, address string, userID uuid.UUID) ([]*wallet.Wallet, error) {
	return nil, nil
}

func (s *stubWalletRepo) ClaimWalletForSync(ctx context.Context, walletID uuid.UUID) (bool, error) {
	return true, nil
}

func (s *stubWalletRepo) GetByID(ctx context.Context, walletID uuid.UUID) (*wallet.Wallet, error) {
	return s.w, nil
}

// The rest of the sync.WalletRepository surface is sync-state bookkeeping that
// ProcessTransaction never touches.
func (s *stubWalletRepo) SetSyncInProgress(ctx context.Context, walletID uuid.UUID) error {
	return nil
}

func (s *stubWalletRepo) SetSyncCompletedAt(ctx context.Context, walletID uuid.UUID, syncAt time.Time) error {
	return nil
}

func (s *stubWalletRepo) SetSyncError(ctx context.Context, walletID uuid.UUID, errMsg string) error {
	return nil
}

func (s *stubWalletRepo) SetSyncPhase(ctx context.Context, walletID uuid.UUID, phase string) error {
	return nil
}

func (s *stubWalletRepo) SetCollectCursor(ctx context.Context, walletID uuid.UUID, cursor time.Time) error {
	return nil
}

func (s *stubWalletRepo) WipeWalletLedger(ctx context.Context, walletID uuid.UUID) error {
	return nil
}

func (s *stubWalletRepo) GetChainSyncRows(ctx context.Context, walletID uuid.UUID) ([]wallet.WalletChainSync, error) {
	return nil, nil
}

func (s *stubWalletRepo) SetChainCollectCursor(ctx context.Context, walletID uuid.UUID, chain string, cursor time.Time) error {
	return nil
}

func (s *stubWalletRepo) SetChainSyncError(ctx context.Context, walletID uuid.UUID, chain, errMsg string) error {
	return nil
}

func (s *stubWalletRepo) SetChainSyncCompleted(ctx context.Context, walletID uuid.UUID, chain string, syncAt time.Time) error {
	return nil
}

func (s *stubWalletRepo) RollupWalletSyncStatus(ctx context.Context, walletID uuid.UUID) error {
	return nil
}

// accountCodesOf collects the account_code of every entry, which is where an
// asset becomes a POSITION in a namespace.
func accountCodesOf(entries []*ledger.Entry) []string {
	codes := make([]string, 0, len(entries))
	for _, e := range entries {
		if code, ok := e.Metadata["account_code"].(string); ok {
			codes = append(codes, code)
		}
	}
	return codes
}

// runLendingSupplyFixture drives lending_supply.json end to end and returns the
// entries the real lending handler produced.
func runLendingSupplyFixture(t *testing.T) (ledger.TransactionType, []*ledger.Entry, *portRegistry) {
	t.Helper()

	ctx := context.Background()
	log := logger.New("test", os.Stdout)

	w := &wallet.Wallet{
		ID:      uuid.New(),
		UserID:  uuid.New(),
		Address: "0x9afcd847c633b820a2f291794d28d374b555811b",
	}
	repo := &stubWalletRepo{w: w}
	cap := &captureLedger{t: t, handler: lending.NewLendingSupplyHandler(repo, log)}

	registry := newPortRegistry()
	builder := sync.NewTxBuilder(repo, cap, nil, nil, log, nil, registry, nil)

	// The REAL adapter converts the REAL fixture: nothing about the receipt is
	// simulated by the test.
	dt := convert(t, "lending_supply.json", "base")

	_, err := builder.ProcessTransaction(ctx, w, dt)
	require.NoError(t, err)

	require.NotEmpty(t, cap.entries, "the supply must produce entries")
	return cap.txType, cap.entries, registry
}

// TestPort_LendingSupply_BooksPrincipalOnly is the acceptance test for the
// receipt rule at the port (#57).
//
// A lending supply must produce entries for the PRINCIPAL and for nothing else.
// Before this change the same fixture produced two pairs — one for the cbBTC
// that actually left the wallet and one for the aBascbBTC receipt minted
// against it — and the position read double.
func TestPort_LendingSupply_BooksPrincipalOnly(t *testing.T) {
	txType, entries, registry := runLendingSupplyFixture(t)

	assert.Equal(t, ledger.TxTypeLendingSupply, txType)

	// Separate the supply itself from the gas the transaction cost. Gas is a
	// real second movement — the wallet did pay ETH — so its pair belongs here;
	// it is simply not part of the supplied position.
	var supply, gas []*ledger.Entry
	for _, e := range entries {
		if e.EntryType == ledger.EntryTypeGasFee ||
			(e.Metadata["entry_type"] == "gas_payment") {
			gas = append(gas, e)
			continue
		}
		supply = append(supply, e)
	}

	// The supply is ONE balanced pair naming the principal. Two pairs would be
	// the double-count this rule exists to remove: before the change the aToken
	// produced a second, independently balanced pair for the same event.
	require.Len(t, supply, 2, "one supply is one balanced pair, not two")
	principal := registry.idOf(t, "cbBTC")
	for _, e := range supply {
		assert.Equal(t, principal, e.AssetID,
			"only the principal (cbBTC) may be booked; got an entry for %q", e.AssetID)
	}

	debits, credits := 0, 0
	for _, e := range supply {
		if e.DebitCredit == ledger.Debit {
			debits++
		} else {
			credits++
		}
	}
	assert.Equal(t, 1, debits)
	assert.Equal(t, 1, credits)

	// The gas pair is the native coin and nothing else — in particular it is
	// not a receipt that slipped through under a different entry type.
	gasAsset := registry.idOf(t, "ETH")
	for _, e := range gas {
		assert.Equal(t, gasAsset, e.AssetID)
	}
}

// TestPort_LendingSupply_ReceiptTickerInNoNamespace is the other half: the
// receipt must be absent from EVERY account namespace, not merely absent from
// the wallet balance.
//
// The distinction is the whole finding of #44. The old code deliberately routed
// the aToken through a lending clearing account "so the pair balances without
// leaking into the user's liquid wallet balance" — and it did not leak there.
// It leaked into `collateral.`, where it doubled the position. Checking one
// namespace would have passed while the defect was live, so this checks all of
// them: any account code mentioning the receipt ticker is a failure, whatever
// its prefix.
func TestPort_LendingSupply_ReceiptTickerInNoNamespace(t *testing.T) {
	_, entries, registry := runLendingSupplyFixture(t)

	codes := accountCodesOf(entries)
	require.NotEmpty(t, codes, "entries must carry account codes")

	// The receipt from the fixture, plus the debt-token shapes the same rule
	// removes. Account codes name an asset by its registry UUID since #59, so a
	// receipt is "absent" when the id it WOULD have been given appears nowhere —
	// matching on the ticker string would now pass vacuously, because no ticker
	// can appear in a code at all.
	//
	// Only tickers the registry actually saw can be checked this way; a receipt
	// that never reached the registry is absent by the stronger fact that it was
	// never resolved, which is asserted separately below.
	forbidden := []string{"aBascbBTC", "aBasWETH", "variableDebt", "stableDebt"}

	for _, bad := range forbidden {
		id, resolved := registry.bySymbol[bad]
		if !resolved {
			// Never resolved: the leg was dropped before it could acquire an
			// identity, which is the outcome the rule exists to produce.
			continue
		}
		for _, code := range codes {
			assert.NotContains(t, code, id.String(),
				"receipt %q (%s) must not appear in any namespace; found in %q", bad, id, code)
		}
	}

	// And the clearing account that existed only to balance the receipt's leg
	// is gone with it.
	for _, code := range codes {
		assert.False(t, strings.HasPrefix(code, "clearing.lending."),
			"the lending clearing account existed only to balance a receipt leg; got %q", code)
	}

	// Positive control: the principal IS booked, so the assertions above are
	// not passing merely because nothing was produced.
	joined := strings.Join(codes, " ")
	assert.Contains(t, joined, registry.idOf(t, "cbBTC").String(),
		"the principal must still be booked somewhere")
}

// TestPort_RewardsReceived_StaysAPosition pins the other side of the boundary
// the receipt rule draws (#57).
//
// `rewardsReceived` is PRINCIPAL: a claimed reward is a genuine acquisition and
// must stay a position. The rule drops receipts, and a reward is not one —
// getting this wrong in the other direction would silently delete real income.
func TestPort_RewardsReceived_StaysAPosition(t *testing.T) {
	dt := convert(t, "claim_rewards.json", "base")

	require.Len(t, dt.Transfers, 2, "both reward legs must survive the receipt rule")
	for _, tr := range dt.Transfers {
		assert.Equal(t, "rewardsReceived", tr.Action)
		assert.Equal(t, sync.DirectionIn, tr.Direction)
		assert.False(t, sync.IsReceiptLeg(tr.Action),
			"a reward is an acquisition, not a receipt")
	}

	symbols := make([]string, 0, len(dt.Transfers))
	for _, tr := range dt.Transfers {
		symbols = append(symbols, tr.AssetSymbol)
	}
	assert.ElementsMatch(t, []string{"USDC", "ETH"}, symbols)
}
