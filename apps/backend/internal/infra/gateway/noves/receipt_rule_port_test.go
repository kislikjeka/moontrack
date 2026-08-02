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
func runLendingSupplyFixture(t *testing.T) (ledger.TransactionType, []*ledger.Entry) {
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

	builder := sync.NewTxBuilder(repo, cap, nil, nil, log, nil, nil, nil, nil)

	// The REAL adapter converts the REAL fixture: nothing about the receipt is
	// simulated by the test.
	dt := convert(t, "lending_supply.json", "base")

	_, err := builder.ProcessTransaction(ctx, w, dt)
	require.NoError(t, err)

	require.NotEmpty(t, cap.entries, "the supply must produce entries")
	return cap.txType, cap.entries
}

// TestPort_LendingSupply_BooksPrincipalOnly is the acceptance test for the
// receipt rule at the port (#57).
//
// A lending supply must produce entries for the PRINCIPAL and for nothing else.
// Before this change the same fixture produced two pairs — one for the cbBTC
// that actually left the wallet and one for the aBascbBTC receipt minted
// against it — and the position read double.
func TestPort_LendingSupply_BooksPrincipalOnly(t *testing.T) {
	txType, entries := runLendingSupplyFixture(t)

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
	for _, e := range supply {
		assert.Equal(t, "cbBTC", e.AssetID,
			"only the principal may be booked; got an entry for %q", e.AssetID)
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
	for _, e := range gas {
		assert.Equal(t, "ETH", e.AssetID)
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
	_, entries := runLendingSupplyFixture(t)

	codes := accountCodesOf(entries)
	require.NotEmpty(t, codes, "entries must carry account codes")

	// The receipt ticker from the fixture, plus the debt-token shape that the
	// same rule removes.
	forbidden := []string{"aBascbBTC", "aBasWETH", "variableDebt", "stableDebt"}

	for _, code := range codes {
		for _, bad := range forbidden {
			assert.NotContains(t, code, bad,
				"receipt ticker %q must not appear in any namespace; found in %q", bad, code)
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
	assert.Contains(t, joined, "cbBTC", "the principal must still be booked somewhere")
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
