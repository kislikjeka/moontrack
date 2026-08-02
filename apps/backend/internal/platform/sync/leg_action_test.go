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

// TestIsReceiptLeg covers the closed vocabulary that decides whether a leg is a
// protocol receipt (#57).
//
// The two halves of the table are the boundary the ticket draws. lpTokensMinted
// sits with collateralSharesMinted — an LP token IS a receipt — while
// rewardsReceived sits on the other side, because a reward is a genuine
// acquisition and stays a position.
func TestIsReceiptLeg(t *testing.T) {
	receipts := []string{
		"collateralSharesMinted",
		"collateralSharesBurned",
		"debtSharesMinted",
		"debtSharesBurned",
		"lpTokensMinted",
		"lpTokenBurned",
	}
	for _, action := range receipts {
		assert.True(t, sync.IsReceiptLeg(action), "%q must be a receipt", action)
	}

	principals := []string{
		"deposited",
		"withdrawn",
		"liquidityAdded",
		"liquidityRemoved",
		"rewardsReceived", // a reward is an acquisition, not a receipt
		"borrowed",
		"repaid",
		"received",
		"sent",
		"bought",
		"paid",
		"bridged",
		"", // no action at all
	}
	for _, action := range principals {
		assert.False(t, sync.IsReceiptLeg(action), "%q must NOT be a receipt", action)
	}
}

// TestIsUnknownLegAction: an action outside all three closed sets is
// unrecognized, and everything inside them is not.
//
// An empty action is deliberately NOT unknown. Legs routinely carry none, and
// reporting those would bury the rare real signal under constant noise.
func TestIsUnknownLegAction(t *testing.T) {
	assert.True(t, sync.IsUnknownLegAction("someBrandNewProtocolAction"))
	assert.True(t, sync.IsUnknownLegAction("vaultSharesMinted"))

	assert.False(t, sync.IsUnknownLegAction(""), "an absent action is not an unrecognized one")
	assert.False(t, sync.IsUnknownLegAction("collateralSharesMinted"))
	assert.False(t, sync.IsUnknownLegAction("rewardsReceived"))
	assert.False(t, sync.IsUnknownLegAction("received"))
	assert.False(t, sync.IsUnknownLegAction("paidGas"))
}

// newLegActionEnv wires a TxBuilder whose logs the test can read.
func newLegActionEnv(t *testing.T) (*sync.TxBuilder, *MockLedgerService, *bytes.Buffer, *wallet.Wallet) {
	t.Helper()

	var logs bytes.Buffer
	log := logger.New("test", &logs)

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil).Maybe()

	ledgerSvc := new(MockLedgerService)
	ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Maybe()

	builder := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, nil, nil)
	w := newTestWallet(uuid.New(), "0x1111111111111111111111111111111111111111")

	return builder, ledgerSvc, &logs, w
}

// legActionTx builds a minimal inbound transaction carrying the given leg
// actions.
func legActionTx(hash string, actions ...string) sync.DecodedTransaction {
	transfers := make([]sync.DecodedTransfer, 0, len(actions))
	for _, a := range actions {
		transfers = append(transfers, sync.DecodedTransfer{
			AssetSymbol:     "USDC",
			ContractAddress: "0xusdc",
			Decimals:        6,
			Amount:          big.NewInt(1000000),
			Direction:       sync.DirectionIn,
			Sender:          "0xpool",
			Recipient:       "0x1111111111111111111111111111111111111111",
			Action:          a,
		})
	}
	return sync.DecodedTransaction{
		ID:            "ext-" + hash,
		TxHash:        hash,
		ChainID:       "base",
		OperationType: sync.OpReceive,
		LegActions:    actions,
		Transfers:     transfers,
		MinedAt:       time.Now(),
		Status:        "confirmed",
	}
}

// TestUnknownLegAction_IsLoggedAndTreatedAsPrincipal is the audit surface for
// the one limitation the receipt rule accepts (#57).
//
// The leg-action vocabulary belongs to the provider and is not frozen. A
// protocol that mints its receipt under an action absent from our closed set
// will have that receipt booked as a position — the same double-count the rule
// removes, reappearing under a name we have not met. Nothing in the data
// separates that from a genuinely new principal action, so it cannot be decided
// automatically; what it must not do is pass in silence.
//
// The leg is still processed as principal. Guessing the other way would delete
// real movements on every unfamiliar protocol, and a lost movement is the one
// outcome the product does not accept.
func TestUnknownLegAction_IsLoggedAndTreatedAsPrincipal(t *testing.T) {
	builder, ledgerSvc, logs, w := newLegActionEnv(t)

	tx := legActionTx("0xunknown", "vaultSharesMinted")

	_, err := builder.ProcessTransaction(context.Background(), w, tx)
	require.NoError(t, err)

	// Logged, naming the action, at WARN.
	assert.Contains(t, logs.String(), "WARN")
	assert.Contains(t, logs.String(), "vaultSharesMinted")
	assert.Contains(t, logs.String(), "0xunknown")

	// Not dropped: the leg reached the ledger as an ordinary inbound movement.
	require.Len(t, ledgerSvc.recordedTransactions, 1)
	assert.Equal(t, ledger.TxTypeTransferIn, ledgerSvc.recordedTransactions[0].TxType)
}

// TestKnownLegActions_AreNotLogged is the mirror: the report must stay quiet on
// everything the vocabulary already covers, or it is useless as a signal.
func TestKnownLegActions_AreNotLogged(t *testing.T) {
	for _, action := range []string{"received", "deposited", "rewardsReceived", "collateralSharesMinted", ""} {
		t.Run("action="+action, func(t *testing.T) {
			builder, _, logs, w := newLegActionEnv(t)

			_, err := builder.ProcessTransaction(context.Background(), w, legActionTx("0xknown", action))
			require.NoError(t, err)

			assert.NotContains(t, logs.String(), "unrecognized provider leg action",
				"a known action must not be reported")
		})
	}
}
