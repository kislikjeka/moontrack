package sync_test

import (
	"context"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pkgsync "github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Issue #29: collection proceeds oldest→newest and the collect cursor is the
// CONTIGUOUS high-water mark — the newest point up to which every earlier
// transaction is durably stored. The boundary is inclusive (`>=`), so the cursor
// transaction is re-fetched next cycle and absorbed by the idempotent upsert.
//
// The property that matters financially: the collector must never advance the
// cursor past a transaction it failed to store. Skipping the OLDEST history
// silently drops the lowest-cost-basis lots, which quietly inflates realized PnL
// forever after.

const hwChain = "ethereum"

func hwTx(id string, minedAt time.Time) pkgsync.DecodedTransaction {
	return pkgsync.DecodedTransaction{
		ID: id, TxHash: "0x" + id, ChainID: hwChain,
		OperationType: pkgsync.OpReceive,
		Transfers: []pkgsync.DecodedTransfer{{
			AssetSymbol: "ETH", AssetName: "Ethereum",
			Decimals: 18, Amount: big.NewInt(1e18),
			Direction: pkgsync.DirectionIn,
		}},
		MinedAt: minedAt, Status: "confirmed",
	}
}

// newTestCollector builds a Collector with test doubles. It moved here when
// collector_test.go was deleted with the extractAssets tests it existed for
// (#59) — the cursor tests below are the remaining users.
func newTestCollector(
	provider pkgsync.TransactionDataProvider,
	rawTxRepo pkgsync.RawTransactionRepository,
	walletRepo pkgsync.WalletRepository,
) *pkgsync.Collector {
	log := logger.New("test", os.Stdout)
	config := pkgsync.DefaultConfig()
	return pkgsync.NewCollector(provider, rawTxRepo, walletRepo, config, log)
}

// hwSetup wires a collector whose wallet syncs exactly one chain, with a
// pre-existing cursor so the run is an incremental resume.
func hwSetup(t *testing.T, ctx context.Context, cursor *time.Time) (
	*pkgsync.Collector, *MockTransactionDataProvider, *MockRawTransactionRepository,
	*MockWalletRepository, *wallet.Wallet,
) {
	return hwSetupFor(t, ctx, uuid.New(), cursor)
}

// hwSetupFor is hwSetup pinned to a specific wallet ID, so a test can run two
// consecutive sync cycles against the same wallet with fresh mocks each time.
func hwSetupFor(t *testing.T, ctx context.Context, walletID uuid.UUID, cursor *time.Time) (
	*pkgsync.Collector, *MockTransactionDataProvider, *MockRawTransactionRepository,
	*MockWalletRepository, *wallet.Wallet,
) {
	t.Helper()

	w := &wallet.Wallet{ID: walletID, Address: "0x1111111111111111111111111111111111111111"}

	provider := new(MockTransactionDataProvider)
	rawTxRepo := new(MockRawTransactionRepository)
	walletRepo := new(MockWalletRepository)

	walletRepo.On("SetSyncPhase", ctx, walletID, mock.Anything).Return(nil)
	walletRepo.On("GetChainSyncRows", ctx, walletID).Return([]wallet.WalletChainSync{
		{WalletID: walletID, Chain: hwChain, SyncStatus: wallet.SyncStatusPending, CollectCursorAt: cursor},
	}, nil)

	return newTestCollector(provider, rawTxRepo, walletRepo),
		provider, rawTxRepo, walletRepo, w
}

// The cursor must stop at the last CONTIGUOUSLY stored transaction. With a
// mid-batch store failure, a max-based cursor would jump to the newest stored tx
// and the failed one would never be re-fetched — silently lost forever.
func TestCollect_CursorStopsAtFirstUnstoredTransaction(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	collector, provider, rawTxRepo, walletRepo, w := hwSetup(t, ctx, nil)

	t1 := hwTx("t1", base)
	t2 := hwTx("t2", base.Add(time.Hour))   // this one fails to store
	t3 := hwTx("t3", base.Add(2*time.Hour)) // stored, but AFTER the gap
	t4 := hwTx("t4", base.Add(3*time.Hour)) // stored, but AFTER the gap

	provider.On("GetTransactions", ctx, w.Address, hwChain, mock.Anything).
		Return([]pkgsync.DecodedTransaction{t1, t2, t3, t4}, nil)

	rawTxRepo.On("UpsertRawTransaction", ctx, mock.MatchedBy(func(r *pkgsync.RawTransaction) bool {
		return r.ExternalID == "t2"
	})).Return(errors.New("transient db failure"))
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)

	walletRepo.On("SetChainCollectCursor", ctx, w.ID, hwChain, mock.Anything).Return(nil)

	_, err := collector.CollectIncremental(ctx, w)
	require.NoError(t, err)

	// The cursor may only reach t1 — the last tx before the gap.
	walletRepo.AssertCalled(t, "SetChainCollectCursor", ctx, w.ID, hwChain, t1.MinedAt)
	for _, skipped := range []pkgsync.DecodedTransaction{t2, t3, t4} {
		walletRepo.AssertNotCalled(t, "SetChainCollectCursor", ctx, w.ID, hwChain, skipped.MinedAt)
	}
}

// When the very first transaction fails, there is no contiguous prefix at all,
// so the cursor must not move — not even to "now".
func TestCollect_CursorUnchangedWhenFirstTransactionFails(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	collector, provider, rawTxRepo, walletRepo, w := hwSetup(t, ctx, nil)

	provider.On("GetTransactions", ctx, w.Address, hwChain, mock.Anything).
		Return([]pkgsync.DecodedTransaction{
			hwTx("t1", base), hwTx("t2", base.Add(time.Hour)),
		}, nil)

	rawTxRepo.On("UpsertRawTransaction", ctx, mock.MatchedBy(func(r *pkgsync.RawTransaction) bool {
		return r.ExternalID == "t1"
	})).Return(errors.New("boom"))
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetChainCollectCursor", ctx, w.ID, hwChain, mock.Anything).Return(nil)

	_, err := collector.CollectIncremental(ctx, w)
	require.NoError(t, err)

	walletRepo.AssertNotCalled(t, "SetChainCollectCursor", ctx, w.ID, hwChain, mock.Anything)
}

// The collector must not trust the provider's ordering: it folds by mined_at, so
// a provider that returns a page out of order still yields a correct high-water
// mark rather than a cursor that leapfrogs unstored history.
func TestCollect_CursorIsOrderIndependent(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	collector, provider, rawTxRepo, walletRepo, w := hwSetup(t, ctx, nil)

	oldest := hwTx("t1", base)
	middle := hwTx("t2", base.Add(time.Hour))
	newest := hwTx("t3", base.Add(2*time.Hour))

	// Provider hands them back newest-first despite sort=asc.
	provider.On("GetTransactions", ctx, w.Address, hwChain, mock.Anything).
		Return([]pkgsync.DecodedTransaction{newest, middle, oldest}, nil)

	// The MIDDLE one fails: after sorting, the contiguous prefix ends at oldest.
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.MatchedBy(func(r *pkgsync.RawTransaction) bool {
		return r.ExternalID == "t2"
	})).Return(errors.New("boom"))
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetChainCollectCursor", ctx, w.ID, hwChain, mock.Anything).Return(nil)

	_, err := collector.CollectIncremental(ctx, w)
	require.NoError(t, err)

	walletRepo.AssertCalled(t, "SetChainCollectCursor", ctx, w.ID, hwChain, oldest.MinedAt)
	walletRepo.AssertNotCalled(t, "SetChainCollectCursor", ctx, w.ID, hwChain, newest.MinedAt)
}

// The window passed to the provider is the chain's own cursor VERBATIM — an
// inclusive `>=` boundary. Nudging it forward by even a nanosecond would skip a
// sibling transaction mined in the same second as the cursor.
func TestCollect_ResumesFromInclusiveCursorBoundary(t *testing.T) {
	ctx := context.Background()
	cursor := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	collector, provider, rawTxRepo, walletRepo, w := hwSetup(t, ctx, &cursor)

	// The cursor transaction itself comes back (inclusive boundary) alongside a
	// same-timestamp sibling that a nudged boundary would have skipped.
	atCursor := hwTx("t-cursor", cursor)
	sibling := hwTx("t-sibling", cursor)
	newer := hwTx("t-newer", cursor.Add(time.Hour))

	var gotSince time.Time
	provider.On("GetTransactions", ctx, w.Address, hwChain, mock.Anything).
		Run(func(args mock.Arguments) { gotSince = args.Get(3).(time.Time) }).
		Return([]pkgsync.DecodedTransaction{atCursor, sibling, newer}, nil)

	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)
	walletRepo.On("SetChainCollectCursor", ctx, w.ID, hwChain, mock.Anything).Return(nil)

	_, err := collector.CollectIncremental(ctx, w)
	require.NoError(t, err)

	assert.Equal(t, cursor, gotSince, "since must be the cursor verbatim (inclusive >=)")
	// Re-storing the duplicate cursor tx is expected and idempotent.
	rawTxRepo.AssertCalled(t, "UpsertRawTransaction", ctx, mock.MatchedBy(func(r *pkgsync.RawTransaction) bool {
		return r.ExternalID == "t-cursor"
	}))
	walletRepo.AssertCalled(t, "SetChainCollectCursor", ctx, w.ID, hwChain, newer.MinedAt)
}

// mockStreamingProvider serves history as several real pages, unlike the shared
// single-page MockTransactionDataProvider. That is what lets these tests exercise
// cross-page behavior: contiguity carried between pages, and a deep sync
// interrupted mid-pagination (exhausted retries, a 5xx, context cancellation)
// after `failAfter` pages have been delivered.
type mockStreamingProvider struct {
	pages      [][]pkgsync.DecodedTransaction
	failAfter  int // number of pages to deliver before failing; -1 = never fail
	sinceSeen  []time.Time
	pagesGiven int
}

// GetTransactions satisfies the port but is unused: the collector always streams.
func (m *mockStreamingProvider) GetTransactions(
	context.Context, string, string, time.Time,
) ([]pkgsync.DecodedTransaction, error) {
	panic("collector must collect via StreamTransactions")
}

func (m *mockStreamingProvider) StreamTransactions(
	ctx context.Context, address, chain string, since time.Time,
	onPage func([]pkgsync.DecodedTransaction) error,
) error {
	m.sinceSeen = append(m.sinceSeen, since)
	for _, page := range m.pages {
		// Honor the inclusive `>=` window the real provider implements.
		var windowed []pkgsync.DecodedTransaction
		for _, tx := range page {
			if since.IsZero() || !tx.MinedAt.Before(since) {
				windowed = append(windowed, tx)
			}
		}
		if len(windowed) == 0 {
			continue
		}
		if m.failAfter >= 0 && m.pagesGiven >= m.failAfter {
			return errors.New("upstream died mid-pagination")
		}
		m.pagesGiven++
		if err := onPage(windowed); err != nil {
			return err
		}
	}
	return nil
}

// A deep sync interrupted mid-pagination must keep every page it already
// collected and resume forward from there. Before per-page persistence, a
// failure on page 3 discarded pages 1 and 2 as well, so a wallet whose backfill
// could not finish inside one cycle would restart forever and never converge.
func TestCollect_StreamingDeepSyncPersistsPagesBeforeInterruption(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	walletID := uuid.New()

	// Three ascending pages, an hour between transactions.
	p1 := []pkgsync.DecodedTransaction{hwTx("a1", base), hwTx("a2", base.Add(time.Hour))}
	p2 := []pkgsync.DecodedTransaction{hwTx("b1", base.Add(2*time.Hour)), hwTx("b2", base.Add(3*time.Hour))}
	p3 := []pkgsync.DecodedTransaction{hwTx("c1", base.Add(4*time.Hour)), hwTx("c2", base.Add(5*time.Hour))}

	// --- Cycle 1: dies after delivering the first two pages ---
	streamer := &mockStreamingProvider{
		pages:     [][]pkgsync.DecodedTransaction{p1, p2, p3},
		failAfter: 2,
	}
	_, _, rawTxRepo, walletRepo, w := hwSetupFor(t, ctx, walletID, nil)
	collector := newTestCollector(streamer, rawTxRepo, walletRepo)

	stored := map[string]bool{}
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			stored[args.Get(1).(*pkgsync.RawTransaction).ExternalID] = true
		}).Return(nil)

	var lastCursor time.Time
	walletRepo.On("SetChainCollectCursor", ctx, walletID, hwChain, mock.Anything).
		Run(func(args mock.Arguments) { lastCursor = args.Get(3).(time.Time) }).Return(nil)
	walletRepo.On("SetChainSyncError", ctx, walletID, hwChain, mock.Anything).Return(nil)

	_, err := collector.CollectIncremental(ctx, w)
	require.NoError(t, err, "a chain failure is isolated, not propagated")

	// The two delivered pages survived the interruption...
	for _, id := range []string{"a1", "a2", "b1", "b2"} {
		assert.True(t, stored[id], "page contents before the failure must be persisted: %s", id)
	}
	// ...and the cursor sits at the high-water mark of what actually landed.
	assert.Equal(t, p2[len(p2)-1].MinedAt, lastCursor)
	// The chain is flagged so the wallet-level rollup reports the failure.
	walletRepo.AssertCalled(t, "SetChainSyncError", ctx, walletID, hwChain, mock.Anything)

	// --- Cycle 2: resumes forward from the cursor and finishes ---
	streamer2 := &mockStreamingProvider{
		pages:     [][]pkgsync.DecodedTransaction{p1, p2, p3},
		failAfter: -1,
	}
	_, _, rawTxRepo2, walletRepo2, w2 := hwSetupFor(t, ctx, walletID, &lastCursor)
	collector2 := newTestCollector(streamer2, rawTxRepo2, walletRepo2)

	rawTxRepo2.On("UpsertRawTransaction", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			stored[args.Get(1).(*pkgsync.RawTransaction).ExternalID] = true
		}).Return(nil)
	var finalCursor time.Time
	walletRepo2.On("SetChainCollectCursor", ctx, walletID, hwChain, mock.Anything).
		Run(func(args mock.Arguments) { finalCursor = args.Get(3).(time.Time) }).Return(nil)

	_, err = collector2.CollectIncremental(ctx, w2)
	require.NoError(t, err)

	// Resumption starts at the cursor, inclusive — not before, not after.
	require.Len(t, streamer2.sinceSeen, 1)
	assert.Equal(t, lastCursor, streamer2.sinceSeen[0])

	// The whole history is now stored: nothing was dropped by the interruption.
	for _, id := range []string{"a1", "a2", "b1", "b2", "c1", "c2"} {
		assert.True(t, stored[id], "transaction %s must survive the interrupted deep sync", id)
	}
	assert.Equal(t, p3[len(p3)-1].MinedAt, finalCursor)
}

// Contiguity must hold ACROSS pages, not just within one. A gap on page 1
// poisons every later page: once a transaction is unstored, no later page may
// push the cursor past it, or the next cycle's `since` skips it forever. This is
// the same silent-skip the single-page tests guard against, one level up.
func TestCollect_CursorContiguityHoldsAcrossPages(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	walletID := uuid.New()

	// Page 1 has the gap; pages 2 and 3 store cleanly.
	p1 := []pkgsync.DecodedTransaction{hwTx("a1", base), hwTx("a2", base.Add(time.Hour))}
	p2 := []pkgsync.DecodedTransaction{hwTx("b1", base.Add(2*time.Hour))}
	p3 := []pkgsync.DecodedTransaction{hwTx("c1", base.Add(3*time.Hour))}

	streamer := &mockStreamingProvider{
		pages:     [][]pkgsync.DecodedTransaction{p1, p2, p3},
		failAfter: -1,
	}
	_, _, rawTxRepo, walletRepo, w := hwSetupFor(t, ctx, walletID, nil)
	collector := newTestCollector(streamer, rawTxRepo, walletRepo)

	// a2 (last tx of page 1) never lands.
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.MatchedBy(func(r *pkgsync.RawTransaction) bool {
		return r.ExternalID == "a2"
	})).Return(errors.New("transient db failure"))
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).Return(nil)

	var cursors []time.Time
	walletRepo.On("SetChainCollectCursor", ctx, walletID, hwChain, mock.Anything).
		Run(func(args mock.Arguments) { cursors = append(cursors, args.Get(3).(time.Time)) }).
		Return(nil)

	_, err := collector.CollectIncremental(ctx, w)
	require.NoError(t, err)

	// The cursor may never pass a2's timestamp, no matter how many clean pages
	// follow — otherwise a2 is dropped from history permanently.
	for _, c := range cursors {
		assert.True(t, c.Before(p1[1].MinedAt),
			"cursor %s advanced past the unstored a2 at %s", c, p1[1].MinedAt)
	}
	if len(cursors) > 0 {
		assert.Equal(t, p1[0].MinedAt, cursors[len(cursors)-1],
			"final cursor must rest at a1, the last contiguously stored tx")
	}
}

// An interrupted deep sync must resume FORWARD from its high-water mark and drop
// nothing: cycle 1 stores the oldest slice then dies, cycle 2 picks up exactly
// where it stopped, and the union covers the whole history with no gap.
func TestCollect_InterruptedDeepSyncResumesForwardWithoutDroppingOldest(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	history := make([]pkgsync.DecodedTransaction, 0, 6)
	for i := 0; i < 6; i++ {
		history = append(history, hwTx(
			"h"+string(rune('0'+i)), base.Add(time.Duration(i)*time.Hour)))
	}

	// --- Cycle 1: dies partway through (tx index 3 fails to store) ---
	collector, provider, rawTxRepo, walletRepo, w := hwSetup(t, ctx, nil)
	provider.On("GetTransactions", ctx, w.Address, hwChain, mock.Anything).Return(history, nil)

	stored := map[string]bool{}
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.MatchedBy(func(r *pkgsync.RawTransaction) bool {
		return r.ExternalID == "h3"
	})).Return(errors.New("interrupted"))
	rawTxRepo.On("UpsertRawTransaction", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			stored[args.Get(1).(*pkgsync.RawTransaction).ExternalID] = true
		}).Return(nil)

	var cycle1Cursor time.Time
	walletRepo.On("SetChainCollectCursor", ctx, w.ID, hwChain, mock.Anything).
		Run(func(args mock.Arguments) { cycle1Cursor = args.Get(3).(time.Time) }).
		Return(nil)

	_, err := collector.CollectIncremental(ctx, w)
	require.NoError(t, err)
	require.Equal(t, history[2].MinedAt, cycle1Cursor, "cursor stops before the failure")

	// --- Cycle 2: resumes from the cursor, this time everything stores ---
	collector2, provider2, rawTxRepo2, walletRepo2, w2 := hwSetupFor(t, ctx, w.ID, &cycle1Cursor)

	// Inclusive boundary → the provider returns h2 onward.
	remaining := history[2:]
	provider2.On("GetTransactions", ctx, w2.Address, hwChain, mock.Anything).Return(remaining, nil)
	rawTxRepo2.On("UpsertRawTransaction", ctx, mock.Anything).
		Run(func(args mock.Arguments) {
			stored[args.Get(1).(*pkgsync.RawTransaction).ExternalID] = true
		}).Return(nil)

	var cycle2Cursor time.Time
	walletRepo2.On("SetChainCollectCursor", ctx, w2.ID, hwChain, mock.Anything).
		Run(func(args mock.Arguments) { cycle2Cursor = args.Get(3).(time.Time) }).
		Return(nil)

	_, err = collector2.CollectIncremental(ctx, w2)
	require.NoError(t, err)

	// Every transaction in the history is durably stored — nothing skipped.
	for _, h := range history {
		assert.True(t, stored[h.ID], "transaction %s must not be dropped by the interruption", h.ID)
	}
	assert.Equal(t, history[len(history)-1].MinedAt, cycle2Cursor, "cursor reaches the newest tx")
}
