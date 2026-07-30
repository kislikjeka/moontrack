package sync_test

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Issue #30 — Unclassified routing at the port seam.
//
// An `unclassified` provider transaction still carries transfers, so it is not
// dropped: it routes through the execute / in-out fallback. The risky case is
// BOTH directions — inferring a swap there would realize phantom PnL on an
// unknown DeFi shape. That case is still recorded (no data loss) but is made
// observable: WARN-logged with tx hash + protocol hint, and tagged on the
// ledger transaction's raw_data so the audit trail survives the log buffer.
// =============================================================================

const unclassifiedWalletAddr = "0x1111111111111111111111111111111111111111"

// newUnclassifiedEnv wires a TxBuilder against a mock ledger with a captured
// log buffer, so a test can assert on both the recorded transaction and the
// WARN trail.
func newUnclassifiedEnv(t *testing.T) (*sync.TxBuilder, *MockLedgerService, *bytes.Buffer, *wallet.Wallet) {
	t.Helper()

	walletRepo := new(MockWalletRepository)
	// No other wallet of this user matches a counterparty → never an internal transfer.
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil).Maybe()

	ledgerSvc := new(MockLedgerService)
	ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil)

	var logs bytes.Buffer
	log := logger.New("test", &logs)

	builder := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, nil)
	w := newTestWallet(uuid.New(), unclassifiedWalletAddr)

	return builder, ledgerSvc, &logs, w
}

// unclassifiedTx builds an `unclassified` decoded transaction with the given
// transfers. Unclassified maps to the execute operation type at the adapter, so
// the port-seam shape is OpExecute + Unclassified.
func unclassifiedTx(id string, transfers ...sync.DecodedTransfer) sync.DecodedTransaction {
	return sync.DecodedTransaction{
		ID:            "base:" + id,
		TxHash:        id,
		ChainID:       "base",
		OperationType: sync.OpExecute,
		Unclassified:  true,
		Transfers:     transfers,
		MinedAt:       time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		Status:        "confirmed",
	}
}

func inTransfer() sync.DecodedTransfer {
	return sync.DecodedTransfer{
		AssetSymbol: "USDC", ContractAddress: "0xusdc", Decimals: 6,
		Amount: big.NewInt(5000000), Direction: sync.DirectionIn,
		Sender: "0x2222222222222222222222222222222222222222", Recipient: unclassifiedWalletAddr,
	}
}

func outTransfer() sync.DecodedTransfer {
	return sync.DecodedTransfer{
		AssetSymbol: "ETH", Decimals: 18,
		Amount: big.NewInt(1e15), Direction: sync.DirectionOut,
		Sender: unclassifiedWalletAddr, Recipient: "0x3333333333333333333333333333333333333333",
	}
}

// TestUnclassified_InOnly_RoutesToTransferIn: an unclassified tx carrying only
// inflow is safe — record it as a plain transfer_in, no warning, no tag.
func TestUnclassified_InOnly_RoutesToTransferIn(t *testing.T) {
	builder, ledgerSvc, logs, w := newUnclassifiedEnv(t)

	_, err := builder.ProcessTransaction(context.Background(), w, unclassifiedTx("0xin", inTransfer()))
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rec := ledgerSvc.recordedTransactions[0]
	assert.Equal(t, ledger.TxTypeTransferIn, rec.TxType)
	assert.NotContains(t, rec.RawData, "unclassified_review",
		"a one-directional unclassified tx is unambiguous — no review tag")
	assert.NotContains(t, logs.String(), "WARN")
}

// TestUnclassified_OutOnly_RoutesToTransferOut: mirror of the in-only case.
func TestUnclassified_OutOnly_RoutesToTransferOut(t *testing.T) {
	builder, ledgerSvc, logs, w := newUnclassifiedEnv(t)

	_, err := builder.ProcessTransaction(context.Background(), w, unclassifiedTx("0xout", outTransfer()))
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rec := ledgerSvc.recordedTransactions[0]
	assert.Equal(t, ledger.TxTypeTransferOut, rec.TxType)
	assert.NotContains(t, rec.RawData, "unclassified_review")
	assert.NotContains(t, logs.String(), "WARN")
}

// TestUnclassified_BothDirections_RecordedWarnedAndTagged is the load-bearing
// case of #30: the transaction is still recorded (no data loss), but it is
// WARN-logged with the tx hash + protocol hint and tagged on raw_data so the
// phantom-PnL risk is auditable rather than silent.
func TestUnclassified_BothDirections_RecordedWarnedAndTagged(t *testing.T) {
	builder, ledgerSvc, logs, w := newUnclassifiedEnv(t)

	tx := unclassifiedTx("0xboth", inTransfer(), outTransfer())
	tx.Protocol = "SomeUnknownDeFi"

	ledgerTxID, err := builder.ProcessTransaction(context.Background(), w, tx)
	require.NoError(t, err)
	require.NotNil(t, ledgerTxID, "the transaction must still be recorded — no data loss")

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rec := ledgerSvc.recordedTransactions[0]

	// Recorded, and the tag rides on the ledger transaction's raw_data (JSONB),
	// which outlives any log retention window.
	assert.Equal(t, true, rec.RawData["unclassified_review"],
		"both-direction unclassified must be tagged for review")
	assert.NotEmpty(t, rec.RawData["unclassified_review_reason"])

	// WARN-logged with tx hash + protocol hint.
	out := logs.String()
	assert.Contains(t, out, "WARN")
	assert.Contains(t, out, "0xboth", "the WARN trail must name the tx hash")
	assert.Contains(t, out, "SomeUnknownDeFi", "the WARN trail must carry the protocol hint")
}

// TestClassified_BothDirections_IsUntaggedSwap guards the blast radius: a
// provider-classified swap (op type trade) is a real swap and must stay a
// clean, untagged swap. Only the UNKNOWN shape is flagged.
func TestClassified_BothDirections_IsUntaggedSwap(t *testing.T) {
	builder, ledgerSvc, logs, w := newUnclassifiedEnv(t)

	tx := unclassifiedTx("0xswap", inTransfer(), outTransfer())
	tx.OperationType = sync.OpTrade
	tx.Unclassified = false

	_, err := builder.ProcessTransaction(context.Background(), w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	rec := ledgerSvc.recordedTransactions[0]
	assert.Equal(t, ledger.TxTypeSwap, rec.TxType)
	assert.NotContains(t, rec.RawData, "unclassified_review",
		"a provider-classified swap is not an unknown shape")
	assert.NotContains(t, logs.String(), "WARN")
}

// TestUnclassified_ExecuteBothDirections_NotUnclassified: an `execute`-shaped
// transaction the provider DID classify (Unclassified false) shares the OpExecute
// fallback path but is not part of the unknown bucket — no tag, no WARN.
func TestUnclassified_ExecuteBothDirections_NotUnclassified(t *testing.T) {
	builder, ledgerSvc, logs, w := newUnclassifiedEnv(t)

	tx := unclassifiedTx("0xexec", inTransfer(), outTransfer())
	tx.Unclassified = false

	_, err := builder.ProcessTransaction(context.Background(), w, tx)
	require.NoError(t, err)

	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeSwap, ledgerSvc.recordedTransactions[0].TxType)
	assert.NotContains(t, ledgerSvc.recordedTransactions[0].RawData, "unclassified_review")
	assert.NotContains(t, logs.String(), "WARN")
}
