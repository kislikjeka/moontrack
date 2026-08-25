//go:build integration

package ledger_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/pkg/testasset"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/postgres"
	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
)

// GetBalance is the only *reader* among the account-code sites: every other
// site builds a code to write an entry, this one builds a code to look an
// account up, and GetAccountByCode matches it with exact string equality.
//
// That asymmetry is why this test exists as its own thing. A producer that
// drifts from the shared constructor creates a stray account — wrong, but at
// least visible in the database, and the golden file catches the shape change.
// A reader that drifts creates nothing: it asks for a code no account carries,
// GetBalance maps the miss onto "no account means zero" and returns zero where
// a balance exists. Silently.
//
// The golden file structurally cannot see this: it collects codes from
// GenerateEntries, and the reader emits no entries. Balance reconciliation
// cannot see it either: the account is internally consistent, it simply never
// gets asked. So the round trip is asserted here directly — write a balance
// through the production code shape, then require that the reader finds it.

// readerProbeHandler writes one balanced pair using the production account-code
// shape, i.e. through accountcode.Wallet, the same constructor GetBalance
// uses to read.
//
// It deliberately does not reuse the package's testHandler: that one emits the
// three-segment "wallet.{id}.{asset}" form with no chain segment, which
// GetBalance would never find. That divergence is intentional elsewhere — those
// tests prove the ledger does not care about the code shape — but it makes that
// handler useless for proving the reader agrees with the producer.
type readerProbeHandler struct {
	ledger.BaseHandler
}

func newReaderProbeHandler() *readerProbeHandler {
	return &readerProbeHandler{
		BaseHandler: ledger.NewBaseHandler(ledger.TxTypeManualIncome),
	}
}

func (h *readerProbeHandler) Handle(ctx context.Context, data map[string]interface{}) ([]*ledger.Entry, error) {
	walletID, err := uuid.Parse(data["wallet_id"].(string))
	if err != nil {
		return nil, err
	}
	chain := data["chain_id"].(string)
	assetID, err := uuid.Parse(data["asset_id"].(string))
	if err != nil {
		return nil, err
	}
	amount, _ := new(big.Int).SetString(data["amount"].(string), 10)

	now := time.Now()
	usd := big.NewInt(5000000000000)

	return []*ledger.Entry{
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Debit,
			EntryType:   ledger.EntryTypeAssetIncrease,
			Amount:      new(big.Int).Set(amount),
			AssetID:     assetID,
			USDRate:     new(big.Int).Set(usd),
			USDValue:    new(big.Int).Set(usd),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"wallet_id":    walletID.String(),
				"chain_id":     chain,
				"account_code": accountcode.Wallet(walletID, accountcode.OnChain(chain, assetID)),
			},
		},
		{
			ID:          uuid.New(),
			DebitCredit: ledger.Credit,
			EntryType:   ledger.EntryTypeIncome,
			Amount:      new(big.Int).Set(amount),
			AssetID:     assetID,
			USDRate:     new(big.Int).Set(usd),
			USDValue:    new(big.Int).Set(usd),
			OccurredAt:  now,
			CreatedAt:   now,
			Metadata: map[string]interface{}{
				"chain_id":     chain,
				"account_code": accountcode.Income(accountcode.OnChain(chain, assetID)),
			},
		},
	}, nil
}

func (h *readerProbeHandler) ValidateData(ctx context.Context, data map[string]interface{}) error {
	return nil
}

// createReaderProbeWallet inserts a wallet against the current wallets schema.
//
// It does not reuse the package's createTestWallet helper: that one still
// inserts a chain_id column, which migration 000011 (multichain wallets)
// dropped, so it fails on any schema at or past that migration. Repairing the
// shared helper is out of scope here — it belongs to the tests that use it —
// but this test must actually run to prove anything, so it carries its own
// fixture.
func createReaderProbeWallet(t *testing.T, ctx context.Context, userID uuid.UUID) uuid.UUID {
	t.Helper()

	walletID := uuid.New()
	_, err := testDB.Pool.Exec(ctx, `
		INSERT INTO wallets (id, user_id, name, address, sync_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'pending', NOW(), NOW())
	`, walletID, userID, "Reader Probe "+walletID.String()[:8], "0x"+walletID.String()[:8])
	require.NoError(t, err)
	return walletID
}

// setupReaderProbe wires a service whose only handler emits production-shaped
// account codes.
func setupReaderProbe(t *testing.T) (*ledger.Service, context.Context) {
	t.Helper()

	ctx := context.Background()
	require.NoError(t, testDB.Reset(ctx))
	seedTestAssets(t, ctx, testDB.Pool)

	repo := postgres.NewLedgerRepository(testDB.Pool)
	registry := ledger.NewRegistry()
	registry.Register(newReaderProbeHandler())

	return ledger.NewService(repo, registry, testLogger()), ctx
}

// TestGetBalance_FindsAccountWrittenByConstructor is the reader half of the
// centralisation proof: a balance written through the production code shape
// must be findable by GetBalance, which builds its lookup key with the same
// constructor.
//
// The assertion that carries the weight is the non-zero one. GetBalance
// swallows a lookup miss and returns zero, so "no error" proves nothing here —
// only a non-zero balance distinguishes "found the account" from "asked for a
// code nothing carries".
func TestGetBalance_FindsAccountWrittenByConstructor(t *testing.T) {
	svc, ctx := setupReaderProbe(t)

	userID := createTestUser(t, ctx, testDB.Pool)
	walletID := createReaderProbeWallet(t, ctx, userID)

	const chain = "ethereum"
	assetID := testasset.BTC
	want := big.NewInt(100000000)

	_, err := svc.RecordTransaction(
		ctx,
		ledger.TxTypeManualIncome,
		"manual",
		nil,
		time.Now().Add(-time.Hour),
		map[string]interface{}{
			"wallet_id": walletID.String(),
			"chain_id":  chain,
			"asset_id":  assetID.String(),
			"amount":    want.String(),
		},
	)
	require.NoError(t, err)

	balance, err := svc.GetBalance(ctx, walletID, chain, assetID)
	require.NoError(t, err)
	require.NotNil(t, balance)

	// Zero here is the exact failure this test exists to catch: the account
	// holds a balance, but the reader built a key that matches nothing.
	require.NotEqual(t, 0, balance.Sign(),
		"GetBalance returned zero for an account that holds %s %s: the reader's "+
			"account code no longer matches the code the producer wrote",
		want, assetID)

	assert.Equal(t, 0, balance.Cmp(want),
		"GetBalance found an account but with the wrong balance: want %s, got %s",
		want, balance)
}

// TestGetBalance_ReaderKeyMatchesProducerKey pins the round trip down to the
// string itself, without a database: whatever code the reader builds for a
// (wallet, chain, asset) triple must be the code a producer writes for the same
// triple. If these two ever diverge, the test above starts depending on
// database state to notice; this one fails on the spot.
func TestGetBalance_ReaderKeyMatchesProducerKey(t *testing.T) {
	walletID := uuid.MustParse("11111111-1111-4111-8111-111111111111")

	assert.Equal(t,
		"wallet.11111111-1111-4111-8111-111111111111.ethereum.b0000000-0000-4000-8000-000000000001",
		accountcode.Wallet(walletID, accountcode.OnChain("ethereum", testasset.BTC)),
		"the wallet account code shape changed; GetBalance and every producer "+
			"must move together, and the golden file must be updated deliberately")
}
