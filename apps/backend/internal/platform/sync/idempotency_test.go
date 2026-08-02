package sync_test

import (
	"bytes"
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// =============================================================================
// Issue #31 — Global chain:txHash idempotency + internal-transfer-aware wipe.
//
// One on-chain event is one ledger transaction, no matter how many of the
// user's wallets saw it. `external_id = chain:txHash` under the global
// UNIQUE(source, external_id) enforces that at the DB; the sync pipeline's job
// is to stay correct on the OTHER side of that constraint — the wallet whose
// raw did not become the ledger transaction.
//
// That non-owning wallet still holds a raw_transaction for the same on-chain
// event, and it must REFERENCE the shared ledger transaction rather than being
// forgotten. The reference is what makes wipe/replay correct: the epic scopes
// the wipe as "any raw_transaction of this wallet references this ledger tx",
// so a raw with a dangling NULL leaves the shared transaction unreachable from
// one of its two participants.
//
// Two ways a wallet ends up non-owning:
//   - duplicate-skip: a shared tx already recorded under another wallet
//     (UNIQUE(source, external_id) → 23505)
//   - internal transfer: recorded once, owned by the outgoing (source) side;
//     the incoming side is skipped by design, before the ledger is touched
// =============================================================================

const (
	idemSourceAddr = "0xaaaa000000000000000000000000000000000001"
	idemDestAddr   = "0xbbbb000000000000000000000000000000000002"
	idemThirdParty = "0xcccc000000000000000000000000000000000003"
)

// duplicateErr is the unique-violation the ledger raises when a second wallet
// tries to record an on-chain event another wallet already recorded.
func duplicateErr() error {
	return &pgconn.PgError{Code: "23505", ConstraintName: "transactions_source_external_id_key"}
}

// idemTx builds a decoded transfer of ETH between two addresses, as seen from
// the perspective of `direction` (the wallet under processing).
func idemTx(txHash string, from, to string, direction sync.TransferDirection) sync.DecodedTransaction {
	op := sync.OpSend
	if direction == sync.DirectionIn {
		op = sync.OpReceive
	}
	return sync.DecodedTransaction{
		ID:            "ethereum:" + txHash,
		TxHash:        txHash,
		ChainID:       "ethereum",
		OperationType: op,
		Transfers: []sync.DecodedTransfer{{
			AssetSymbol: "ETH",
			Decimals:    18,
			Amount:      big.NewInt(1e18),
			Direction:   direction,
			Sender:      from,
			Recipient:   to,
		}},
		MinedAt: time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC),
		Status:  "confirmed",
	}
}

// idemEnv wires a TxBuilder whose wallet lookup knows about a fixed set of the
// user's own wallets, so internal-transfer detection can be switched on by
// simply registering the counterparty address.
type idemEnv struct {
	builder   *sync.TxBuilder
	ledgerSvc *MockLedgerService
	walletSvc *MockWalletRepository
}

func newIdemEnv(t *testing.T, userID uuid.UUID, owned map[string]*wallet.Wallet) *idemEnv {
	t.Helper()

	walletRepo := new(MockWalletRepository)
	for addr, w := range owned {
		walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, addr, userID).
			Return([]*wallet.Wallet{w}, nil).Maybe()
	}
	// Any address that is not one of the user's own wallets resolves to nothing.
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil).Maybe()

	ledgerSvc := new(MockLedgerService)
	log := logger.New("test", os.Stdout)

	return &idemEnv{
		builder:   sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, nil, nil),
		ledgerSvc: ledgerSvc,
		walletSvc: walletRepo,
	}
}

// -----------------------------------------------------------------------------
// AC1 — Same chain:txHash under two wallets yields exactly one ledger transaction
// -----------------------------------------------------------------------------

// TestGlobalIdempotency_SameTxTwoWallets_OneLedgerTx: two of the user's wallets
// both see the same on-chain transaction (e.g. both are parties, or the same
// address is tracked twice). The first records it; the second's insert violates
// UNIQUE(source, external_id) and is absorbed. Exactly one ledger transaction
// exists, and — critically — the second wallet learns WHICH transaction it was,
// so its raw can reference it.
func TestGlobalIdempotency_SameTxTwoWallets_OneLedgerTx(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	walletA := newTestWallet(userID, idemSourceAddr)
	walletB := newTestWallet(userID, idemDestAddr)

	env := newIdemEnv(t, userID, nil)

	sharedTxID := uuid.New()
	externalID := "ethereum:0xshared"

	// Wallet A is first: the ledger accepts the insert.
	env.ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: sharedTxID}, nil).Once()

	// Wallet B is second: the same external_id collides.
	env.ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, duplicateErr()).Once()

	// The duplicate path resolves the winner so the loser can point at it.
	env.ledgerSvc.On("FindBySourceExternalID", mock.Anything, "noves", externalID).
		Return(&ledger.Transaction{ID: sharedTxID}, nil).Once()

	// Both wallets process a raw for the same on-chain event. Neither is an
	// internal transfer here — the counterparty is a third party for A, and B
	// simply tracks the same movement.
	txA := idemTx("0xshared", idemSourceAddr, idemThirdParty, sync.DirectionOut)
	txB := idemTx("0xshared", idemSourceAddr, idemThirdParty, sync.DirectionOut)

	gotA, err := env.builder.ProcessTransaction(ctx, walletA, txA)
	require.NoError(t, err)
	require.NotNil(t, gotA, "first wallet owns the ledger transaction")
	assert.Equal(t, sharedTxID, *gotA)

	gotB, err := env.builder.ProcessTransaction(ctx, walletB, txB)
	require.NoError(t, err, "duplicate must be a silent skip, never an error")
	require.NotNil(t, gotB,
		"the duplicate-skipping wallet must still learn the shared ledger tx id, "+
			"otherwise its raw cannot reference it and wipe/replay loses the link")
	assert.Equal(t, sharedTxID, *gotB, "both wallets point at the same ledger transaction")

	// Exactly one insert was accepted: two attempts, one survivor.
	require.Len(t, env.ledgerSvc.recordedTransactions, 2, "both wallets attempt the insert")
	assert.Equal(t, externalID, *env.ledgerSvc.recordedTransactions[0].ExternalID)
	assert.Equal(t, externalID, *env.ledgerSvc.recordedTransactions[1].ExternalID,
		"both attempts use the identical chain:txHash external id — that is what makes them collide")
	env.ledgerSvc.AssertExpectations(t)
}

// TestGlobalIdempotency_ExternalIDIsChainTxHash: the idempotency key stamped on
// the ledger is the globally-unique on-chain identity, not anything wallet- or
// provider-scoped. Two wallets deriving it independently must land on the same
// string, or UNIQUE(source, external_id) never fires.
func TestGlobalIdempotency_ExternalIDIsChainTxHash(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, idemSourceAddr)

	env := newIdemEnv(t, userID, nil)
	env.ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: uuid.New()}, nil).Once()

	_, err := env.builder.ProcessTransaction(ctx, w, idemTx("0xabc", idemSourceAddr, idemThirdParty, sync.DirectionOut))
	require.NoError(t, err)

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	rec := env.ledgerSvc.recordedTransactions[0]
	assert.Equal(t, "noves", rec.Source)
	require.NotNil(t, rec.ExternalID)
	assert.Equal(t, "ethereum:0xabc", *rec.ExternalID,
		"external_id must be chain:txHash so the same on-chain event collides across wallets")
}

// -----------------------------------------------------------------------------
// AC2 — Internal transfer recorded once, owned by the source side
// -----------------------------------------------------------------------------

// TestInternalTransfer_OwnedBySourceSide: when both parties are the user's own
// wallets, the outgoing side records the internal_transfer and the incoming
// side does not — one movement, one ledger row, no phantom PnL.
func TestInternalTransfer_OwnedBySourceSide(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, idemSourceAddr)
	dstWallet := newTestWallet(userID, idemDestAddr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		idemSourceAddr: srcWallet,
		idemDestAddr:   dstWallet,
	})

	internalTxID := uuid.New()
	env.ledgerSvc.On("RecordTransaction", mock.Anything, ledger.TxTypeInternalTransfer, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: internalTxID}, nil).Once()

	// Source side: the wallet is the sender.
	outTx := idemTx("0xinternal", idemSourceAddr, idemDestAddr, sync.DirectionOut)
	gotSrc, err := env.builder.ProcessTransaction(ctx, srcWallet, outTx)
	require.NoError(t, err)
	require.NotNil(t, gotSrc, "the source side owns the internal transfer")
	assert.Equal(t, internalTxID, *gotSrc)

	require.Len(t, env.ledgerSvc.recordedTransactions, 1)
	rec := env.ledgerSvc.recordedTransactions[0]
	assert.Equal(t, ledger.TxTypeInternalTransfer, rec.TxType)
	assert.Equal(t, srcWallet.ID.String(), rec.RawData["source_wallet_id"],
		"ownership is the outgoing side: transactions.wallet_id derives from source_wallet_id")
	assert.Equal(t, dstWallet.ID.String(), rec.RawData["dest_wallet_id"])

	env.ledgerSvc.AssertExpectations(t)
}

// TestInternalTransfer_IncomingSide_ReferencesSharedTx: the incoming side is
// skipped by design — but skipping must not mean forgetting. The destination
// wallet's raw has to come away pointing at the internal_transfer the source
// side recorded, because that reference is the only thing tying the shared
// transaction back to the destination wallet for wipe and replay.
func TestInternalTransfer_IncomingSide_ReferencesSharedTx(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, idemSourceAddr)
	dstWallet := newTestWallet(userID, idemDestAddr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		idemSourceAddr: srcWallet,
		idemDestAddr:   dstWallet,
	})

	internalTxID := uuid.New()
	// The source side already recorded it in a previous (or concurrent) run.
	env.ledgerSvc.On("FindBySourceExternalID", mock.Anything, "noves", "ethereum:0xinternal").
		Return(&ledger.Transaction{ID: internalTxID}, nil).Once()

	// Destination side: the wallet is the recipient.
	inTx := idemTx("0xinternal", idemSourceAddr, idemDestAddr, sync.DirectionIn)
	gotDst, err := env.builder.ProcessTransaction(ctx, dstWallet, inTx)
	require.NoError(t, err)

	require.NotNil(t, gotDst,
		"the incoming side must reference the shared internal_transfer; a NULL here "+
			"orphans the destination wallet from the transaction that credited it")
	assert.Equal(t, internalTxID, *gotDst)

	assert.Empty(t, env.ledgerSvc.recordedTransactions,
		"the incoming side must never record a second ledger transaction")
	env.ledgerSvc.AssertExpectations(t)
}

// TestInternalTransfer_IncomingSide_SourceNotYetRecorded: the destination wallet
// may sync BEFORE the source wallet, so the shared transaction does not exist
// yet. The raw must stay pending — not be skipped with a dangling reference —
// so a later cycle picks it up once the source side has recorded it.
func TestInternalTransfer_IncomingSide_SourceNotYetRecorded(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, idemSourceAddr)
	dstWallet := newTestWallet(userID, idemDestAddr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		idemSourceAddr: srcWallet,
		idemDestAddr:   dstWallet,
	})

	// Nothing recorded yet under this external id.
	env.ledgerSvc.On("FindBySourceExternalID", mock.Anything, "noves", "ethereum:0xinternal").
		Return(nil, nil).Once()

	inTx := idemTx("0xinternal", idemSourceAddr, idemDestAddr, sync.DirectionIn)
	gotDst, err := env.builder.ProcessTransaction(ctx, dstWallet, inTx)

	require.ErrorIs(t, err, sync.ErrSharedTxPending,
		"a not-yet-recorded counterpart is a deferral, distinguishable from both "+
			"success and failure so the processor can leave the raw pending")
	assert.Nil(t, gotDst, "no reference exists yet, so none is claimed")

	assert.Empty(t, env.ledgerSvc.recordedTransactions,
		"the incoming side must never record the transaction itself")
}

// -----------------------------------------------------------------------------
// AC3 (Go half) — the raw carries the reference, so the wipe can find it
//
// `wipe_wallet_ledger` scopes itself to "any raw_transaction of this wallet
// references this ledger tx". These tests pin the Go side of that contract: a
// non-owning wallet's raw must end up marked processed WITH the shared ledger
// tx id, never skipped with a dangling NULL. The SQL half is covered by
// migration 000030 and its up/down verification.
// -----------------------------------------------------------------------------

// newIdemProcessor wires a Processor over the given raw store and TxBuilder.
func newIdemProcessor(rawTxRepo *MockRawTransactionRepository, walletRepo *MockWalletRepository, builder *sync.TxBuilder) *sync.Processor {
	log := logger.New("test", os.Stdout)
	return sync.NewProcessor(rawTxRepo, walletRepo, builder, log)
}

// idemRaw wraps a decoded transaction as a pending raw_transaction row.
func idemRaw(walletID uuid.UUID, dt sync.DecodedTransaction) *sync.RawTransaction {
	return &sync.RawTransaction{
		ID:               uuid.New(),
		WalletID:         walletID,
		ExternalID:       dt.ID,
		TxHash:           dt.TxHash,
		ChainID:          dt.ChainID,
		OperationType:    string(dt.OperationType),
		MinedAt:          dt.MinedAt,
		Status:           dt.Status,
		RawJSON:          marshalDecodedTx(dt),
		ProcessingStatus: sync.ProcessingStatusPending,
	}
}

// TestNonOwningRaw_MarkedProcessedWithSharedTxID: the destination wallet's raw
// for an internal transfer must be marked PROCESSED against the shared ledger
// transaction. Marking it merely "skipped" would leave ledger_tx_id NULL, and
// the wipe — which reaches the shared transaction through exactly this
// reference — could no longer re-pend it from the destination side.
func TestNonOwningRaw_MarkedProcessedWithSharedTxID(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, idemSourceAddr)
	dstWallet := newTestWallet(userID, idemDestAddr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		idemSourceAddr: srcWallet,
		idemDestAddr:   dstWallet,
	})

	internalTxID := uuid.New()
	env.ledgerSvc.On("FindBySourceExternalID", mock.Anything, "noves", "ethereum:0xinternal").
		Return(&ledger.Transaction{ID: internalTxID}, nil).Once()

	inTx := idemTx("0xinternal", idemSourceAddr, idemDestAddr, sync.DirectionIn)
	raw := idemRaw(dstWallet.ID, inTx)

	rawTxRepo := new(MockRawTransactionRepository)
	rawTxRepo.On("GetPendingByWallet", mock.Anything, dstWallet.ID).
		Return([]*sync.RawTransaction{raw}, nil).Once()
	rawTxRepo.On("MarkProcessed", mock.Anything, raw.ID, internalTxID).Return(nil).Once()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("SetSyncPhase", mock.Anything, dstWallet.ID, mock.Anything).Return(nil)
	walletRepo.On("SetSyncCompletedAt", mock.Anything, dstWallet.ID, mock.Anything).Return(nil)

	proc := newIdemProcessor(rawTxRepo, walletRepo, env.builder)
	require.NoError(t, proc.ProcessAll(ctx, dstWallet))

	rawTxRepo.AssertCalled(t, "MarkProcessed", mock.Anything, raw.ID, internalTxID)
	rawTxRepo.AssertNotCalled(t, "MarkSkipped", mock.Anything, mock.Anything, mock.Anything)
}

// TestNonOwningRaw_SourceNotYetRecorded_StaysPending: when the counterpart
// wallet has not synced the event yet, there is nothing to reference. The raw
// must stay PENDING so a later cycle resolves it — marking it skipped would
// strand the destination side of a real transfer forever.
func TestNonOwningRaw_SourceNotYetRecorded_StaysPending(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, idemSourceAddr)
	dstWallet := newTestWallet(userID, idemDestAddr)

	env := newIdemEnv(t, userID, map[string]*wallet.Wallet{
		idemSourceAddr: srcWallet,
		idemDestAddr:   dstWallet,
	})

	env.ledgerSvc.On("FindBySourceExternalID", mock.Anything, "noves", "ethereum:0xinternal").
		Return(nil, nil).Once()

	inTx := idemTx("0xinternal", idemSourceAddr, idemDestAddr, sync.DirectionIn)
	raw := idemRaw(dstWallet.ID, inTx)

	rawTxRepo := new(MockRawTransactionRepository)
	rawTxRepo.On("GetPendingByWallet", mock.Anything, dstWallet.ID).
		Return([]*sync.RawTransaction{raw}, nil).Once()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("SetSyncPhase", mock.Anything, dstWallet.ID, mock.Anything).Return(nil)
	walletRepo.On("SetSyncCompletedAt", mock.Anything, dstWallet.ID, mock.Anything).Return(nil).Maybe()

	proc := newIdemProcessor(rawTxRepo, walletRepo, env.builder)
	require.NoError(t, proc.ProcessAll(ctx, dstWallet))

	rawTxRepo.AssertNotCalled(t, "MarkProcessed", mock.Anything, mock.Anything, mock.Anything)
	rawTxRepo.AssertNotCalled(t, "MarkSkipped", mock.Anything, mock.Anything, mock.Anything)
	rawTxRepo.AssertNotCalled(t, "MarkError", mock.Anything, mock.Anything, mock.Anything)
}

// TestNonOwningRaw_DeferredAfterPreviousSync_WarnsAboutStall: a deferral on a
// wallet that has synced before is no longer an ordinary ordering race — the
// counterpart has had a cycle to appear. It may be a wallet the user deleted or
// a chain they never enabled, which would leave this raw pending indefinitely.
// The raw still stays pending (nothing is lost), but the stall becomes visible
// instead of hiding at Debug level.
func TestNonOwningRaw_DeferredAfterPreviousSync_WarnsAboutStall(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	srcWallet := newTestWallet(userID, idemSourceAddr)
	dstWallet := newTestWallet(userID, idemDestAddr)
	synced := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	dstWallet.LastSyncAt = &synced // this wallet has synced before

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, idemSourceAddr, userID).
		Return([]*wallet.Wallet{srcWallet}, nil).Maybe()
	walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
		Return([]*wallet.Wallet{}, nil).Maybe()
	walletRepo.On("SetSyncPhase", mock.Anything, dstWallet.ID, mock.Anything).Return(nil)
	walletRepo.On("SetSyncCompletedAt", mock.Anything, dstWallet.ID, mock.Anything).Return(nil).Maybe()

	ledgerSvc := new(MockLedgerService)
	ledgerSvc.On("FindBySourceExternalID", mock.Anything, "noves", "ethereum:0xinternal").
		Return(nil, nil).Once()

	var logs bytes.Buffer
	log := logger.New("test", &logs)
	builder := sync.NewTxBuilder(walletRepo, ledgerSvc, nil, nil, log, nil, nil, nil)

	inTx := idemTx("0xinternal", idemSourceAddr, idemDestAddr, sync.DirectionIn)
	raw := idemRaw(dstWallet.ID, inTx)

	rawTxRepo := new(MockRawTransactionRepository)
	rawTxRepo.On("GetPendingByWallet", mock.Anything, dstWallet.ID).
		Return([]*sync.RawTransaction{raw}, nil).Once()

	proc := sync.NewProcessor(rawTxRepo, walletRepo, builder, log)
	require.NoError(t, proc.ProcessAll(ctx, dstWallet))

	assert.Contains(t, logs.String(), "still deferred after a previous sync",
		"a persistent deferral must be visible, not silent")
	assert.Contains(t, logs.String(), "WARN")

	// Still pending: visibility must not cost us the retry.
	rawTxRepo.AssertNotCalled(t, "MarkSkipped", mock.Anything, mock.Anything, mock.Anything)
	rawTxRepo.AssertNotCalled(t, "MarkError", mock.Anything, mock.Anything, mock.Anything)
}

// -----------------------------------------------------------------------------
// AC4 — Demoable via the port seam
//
// The epic names TransactionDataProvider as the preferred seam: drive the full
// collect → reconcile → process pipeline from a fake provider and assert on the
// ledger outcome. This is the whole story end to end — the same on-chain event
// delivered by the provider under two different wallets produces exactly one
// ledger transaction, and the second wallet's raw references it.
// -----------------------------------------------------------------------------

// TestPortSeam_SameEventTwoWallets_OneLedgerTx drives Service.SyncWallet twice,
// once per wallet, with the provider returning the same chain:txHash both times.
func TestPortSeam_SameEventTwoWallets_OneLedgerTx(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	walletA := newTestWallet(userID, idemSourceAddr)
	walletB := newTestWallet(userID, idemDestAddr)

	// The one on-chain event, as each wallet's provider reports it.
	sharedTx := idemTx("0xshared", idemSourceAddr, idemThirdParty, sync.DirectionOut)

	sharedTxID := uuid.New()
	externalID := "ethereum:0xshared"

	ledgerSvc := new(MockLedgerService)
	// Wallet A wins the insert; wallet B collides on UNIQUE(source, external_id).
	ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(&ledger.Transaction{ID: sharedTxID}, nil).Once()
	ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, duplicateErr()).Once()
	ledgerSvc.On("FindBySourceExternalID", mock.Anything, "noves", externalID).
		Return(&ledger.Transaction{ID: sharedTxID}, nil).Once()

	// Track what each wallet's raw ends up referencing.
	referenced := map[uuid.UUID]uuid.UUID{}

	syncOneWallet := func(w *wallet.Wallet) {
		provider := new(MockTransactionDataProvider)
		expectChainTxs(provider, ctx, w.Address, mock.Anything, []sync.DecodedTransaction{sharedTx})

		walletRepo := new(MockWalletRepository)
		walletRepo.On("GetWalletsForSync", mock.Anything).Return([]*wallet.Wallet{w}, nil)
		walletRepo.On("GetWalletsByAddressAndUserID", mock.Anything, mock.Anything, mock.Anything).
			Return([]*wallet.Wallet{}, nil).Maybe()
		walletRepo.On("ClaimWalletForSync", ctx, w.ID).Return(true, nil)
		walletRepo.On("SetSyncInProgress", ctx, w.ID).Return(nil)
		walletRepo.On("SetSyncPhase", ctx, w.ID, mock.Anything).Return(nil)
		walletRepo.On("SetCollectCursor", ctx, w.ID, mock.Anything).Return(nil)
		walletRepo.On("SetSyncCompletedAt", ctx, w.ID, mock.Anything).Return(nil)
		walletRepo.On("SetSyncError", ctx, w.ID, mock.Anything).Return(nil).Maybe()
		expectSingleChainSet(walletRepo, ctx, w.ID)

		// The collector stores the raw; the processor then reads it back.
		stored := idemRaw(w.ID, sharedTx)
		rawTxRepo := new(MockRawTransactionRepository)
		rawTxRepo.On("UpsertRawTransaction", mock.Anything, mock.Anything).Return(nil)
		rawTxRepo.On("GetPendingByWallet", mock.Anything, w.ID).
			Return([]*sync.RawTransaction{stored}, nil)
		rawTxRepo.On("MarkProcessed", mock.Anything, stored.ID, mock.Anything).
			Run(func(args mock.Arguments) {
				referenced[w.ID] = args.Get(2).(uuid.UUID)
			}).Return(nil)

		svc := newTestService(walletRepo, ledgerSvc, provider, nil, rawTxRepo)
		require.NoError(t, svc.SyncWallet(ctx, w.ID))
	}

	syncOneWallet(walletA)
	syncOneWallet(walletB)

	// Exactly one ledger transaction survives, and BOTH wallets' raws point at it.
	require.Len(t, ledgerSvc.recordedTransactions, 2, "both wallets attempt the insert")
	assert.Equal(t, externalID, *ledgerSvc.recordedTransactions[0].ExternalID)
	assert.Equal(t, externalID, *ledgerSvc.recordedTransactions[1].ExternalID)

	assert.Equal(t, sharedTxID, referenced[walletA.ID], "owning wallet references the transaction")
	assert.Equal(t, sharedTxID, referenced[walletB.ID],
		"non-owning wallet references the SAME transaction — this is what makes "+
			"wiping either wallet reach it")
	ledgerSvc.AssertExpectations(t)
}

// TestDuplicateSkip_LookupFailure_Surfaces: if the duplicate winner cannot be
// resolved, the raw must NOT be quietly marked done with a dangling NULL — the
// error surfaces so the raw is retried on the next cycle.
func TestDuplicateSkip_LookupFailure_Surfaces(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	w := newTestWallet(userID, idemSourceAddr)

	env := newIdemEnv(t, userID, nil)
	env.ledgerSvc.On("RecordTransaction", mock.Anything, mock.Anything, "noves", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, duplicateErr()).Once()
	env.ledgerSvc.On("FindBySourceExternalID", mock.Anything, "noves", "ethereum:0xboom").
		Return(nil, assert.AnError).Once()

	_, err := env.builder.ProcessTransaction(ctx, w, idemTx("0xboom", idemSourceAddr, idemThirdParty, sync.DirectionOut))
	require.Error(t, err,
		"an unresolvable duplicate must surface, not silently drop the reference")
}
