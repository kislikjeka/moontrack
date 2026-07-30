package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Collector handles Phase 1: collecting raw transactions from the sync provider
type Collector struct {
	txProvider TransactionDataProvider
	rawTxRepo  RawTransactionRepository
	walletRepo WalletRepository
	assetRepo  SyncAssetRepository
	config     *Config
	logger     *logger.Logger
}

// NewCollector creates a new Collector
func NewCollector(
	txProvider TransactionDataProvider,
	rawTxRepo RawTransactionRepository,
	walletRepo WalletRepository,
	assetRepo SyncAssetRepository,
	config *Config,
	log *logger.Logger,
) *Collector {
	return &Collector{
		txProvider: txProvider,
		rawTxRepo:  rawTxRepo,
		walletRepo: walletRepo,
		assetRepo:  assetRepo,
		config:     config,
		logger:     log.WithField("component", "collector"),
	}
}

// CollectAll performs initial full collection of all transactions.
func (c *Collector) CollectAll(ctx context.Context, w *wallet.Wallet) (int, error) {
	return c.collect(ctx, w, true)
}

// CollectIncremental collects only new transactions since each chain's cursor.
func (c *Collector) CollectIncremental(ctx context.Context, w *wallet.Wallet) (int, error) {
	return c.collect(ctx, w, false)
}

// chainSince computes the `since` window for a single chain, per issue #28: each
// chain resumes from ITS OWN collect cursor, so a chain that fell behind (e.g.
// failed last cycle) can never be dragged forward past its history by a faster
// sibling. When the chain has no cursor yet, we fall back to the wallet's last
// sync (incremental) and finally to the initial-lookback window.
//
// The cursor is returned VERBATIM because the boundary is inclusive (`>=`,
// confirmed against the live Noves API in issue #29). The transaction sitting
// exactly on the cursor is re-fetched and absorbed by the idempotent upsert.
// Nudging the boundary forward to dodge that duplicate would silently skip any
// sibling transaction mined at the very same timestamp — a real case on chains
// where several of a wallet's transactions land in one block.
func (c *Collector) chainSince(w *wallet.Wallet, cr wallet.WalletChainSync, isInitial bool) time.Time {
	if cr.CollectCursorAt != nil {
		return *cr.CollectCursorAt
	}
	if !isInitial && w.LastSyncAt != nil {
		return *w.LastSyncAt
	}
	if c.config.InitialSyncLookback > 0 {
		return time.Now().Add(-c.config.InitialSyncLookback)
	}
	return time.Time{}
}

// collect fans out over the wallet's chain set (the rows of wallet_chain_sync ARE
// the chains this wallet syncs, issue #27) and collects each chain INDEPENDENTLY
// (issue #28): every chain resumes from its own cursor and a chain that errors is
// isolated — its row is marked error, its cursor is left untouched (so it resumes
// where it left off next cycle), and the loop continues to the remaining chains.
// A single chain's failure never aborts the others or corrupts their state.
func (c *Collector) collect(ctx context.Context, w *wallet.Wallet, isInitial bool) (int, error) {
	if err := c.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseCollecting)); err != nil {
		return 0, fmt.Errorf("failed to set sync phase: %w", err)
	}

	chainRows, err := c.walletRepo.GetChainSyncRows(ctx, w.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to load wallet chain set: %w", err)
	}

	count := 0
	fetched := 0
	failedChains := 0

	for _, cr := range chainRows {
		since := c.chainSince(w, cr, isInitial)

		c.logger.Info("collecting chain transactions",
			"wallet_id", w.ID,
			"address", w.Address,
			"chain", cr.Chain,
			"since", since,
			"is_initial", isInitial)

		stored, pageFetched, err := c.collectChain(ctx, w, cr.Chain, since)
		count += stored
		fetched += pageFetched

		if err != nil {
			// Failure isolation: mark ONLY this chain errored and move on. The
			// chain's cursor is deliberately left untouched past its high-water
			// mark, so it resumes exactly where it stopped next cycle without
			// skipping history or disturbing its siblings.
			c.logger.Warn("chain collect failed, isolating and continuing",
				"wallet_id", w.ID,
				"chain", cr.Chain,
				"stored_before_failure", stored,
				"error", err)
			if serr := c.walletRepo.SetChainSyncError(ctx, w.ID, cr.Chain,
				fmt.Sprintf("collect failed: %v", err)); serr != nil {
				c.logger.Error("failed to mark chain sync error",
					"wallet_id", w.ID, "chain", cr.Chain, "error", serr)
			}
			failedChains++
			continue
		}
	}

	c.logger.Info("collection complete",
		"wallet_id", w.ID,
		"chains", len(chainRows),
		"failed_chains", failedChains,
		"stored", count,
		"total_fetched", fetched)

	return count, nil
}

// collectChain collects one chain's history from `since` (inclusive) and returns
// the number of transactions stored, the number fetched, and any fetch error.
//
// Each page is persisted and the chain's cursor advanced before the next page is
// requested (issue #29). That is what makes an interrupted deep sync resumable:
// whatever failure ends the stream — context cancellation, exhausted retries, a
// mid-pagination 5xx — everything collected so far is already durable and the
// cursor already points at the contiguous high-water mark, so the next cycle
// picks up from there instead of restarting the backfill.
//
// A failure to fetch is returned to the caller for per-chain isolation, but the
// pages that DID land stay stored and the cursor keeps its advance — partial
// progress is never rolled back.
func (c *Collector) collectChain(
	ctx context.Context,
	w *wallet.Wallet,
	chain string,
	since time.Time,
) (stored int, fetched int, err error) {
	// contiguous tracks whether EVERY transaction seen so far on this chain — across
	// all pages, not just the current one — was stored. It is deliberately scoped to
	// the whole stream: once a page leaves a gap, no later page may advance the
	// cursor past it, or the next cycle's `since` would jump over the missing
	// transaction and drop it from history permanently.
	contiguous := true

	onPage := func(page []DecodedTransaction) error {
		// Extract asset metadata for the page before storing its raw txs.
		c.extractAssets(ctx, page)
		fetched += len(page)

		pageStored, highWater, pageContiguous := c.storeAscending(ctx, w, chain, page)
		stored += pageStored

		// Advance ONLY this chain's collect cursor, and only to its CONTIGUOUS
		// high-water mark. Nothing wallet-level: each chain owns its own
		// resumption point (issue #28). Once contiguity is broken on any earlier
		// page, the cursor freezes for the rest of the stream.
		if contiguous && highWater != nil {
			if err := c.walletRepo.SetChainCollectCursor(ctx, w.ID, chain, *highWater); err != nil {
				// A cursor write that fails leaves the cursor behind the data we
				// just stored. Freeze it here too: letting a later page advance it
				// would skip everything between the two marks.
				c.logger.Error("failed to update chain collect cursor, freezing it for this cycle",
					"wallet_id", w.ID, "chain", chain, "error", err)
				contiguous = false
			}
		}
		if !pageContiguous {
			contiguous = false
		}
		return nil
	}

	if err := c.txProvider.StreamTransactions(ctx, w.Address, chain, since, onPage); err != nil {
		return stored, fetched, err
	}
	return stored, fetched, nil
}

// storeAscending persists one page of a chain's transactions oldest→newest. It
// returns how many were stored, the page's CONTIGUOUS high-water mark — the
// mined_at of the newest transaction such that it and every older transaction in
// the page are durably stored — and whether the page was fully contiguous (every
// transaction stored). The high-water mark is nil when the page is empty or its
// very first transaction failed, meaning the cursor must not move at all.
//
// The contiguous flag is what lets the caller carry the invariant ACROSS pages:
// a gap on page 1 must freeze the cursor for every page that follows.
//
// Contiguity is the whole point (issue #29). Taking the plain maximum over
// whatever happened to store would let a single mid-batch failure push the
// cursor past the failed transaction, and because the next run resumes from the
// cursor that transaction would never be fetched again — silently dropping
// history. Dropping the OLDEST history is the expensive kind: those are the
// lowest-cost-basis lots, so losing them permanently overstates realized PnL.
// Stopping at the gap instead costs only a re-fetch of the tail next cycle,
// which the idempotent upsert absorbs.
//
// The batch is sorted by mined_at first: ascending order is an invariant we
// enforce, not a provider promise we trust. A provider that ignored sort=asc
// would otherwise let a "contiguous" prefix span an unstored older transaction.
func (c *Collector) storeAscending(
	ctx context.Context,
	w *wallet.Wallet,
	chain string,
	txs []DecodedTransaction,
) (int, *time.Time, bool) {
	ordered := make([]DecodedTransaction, len(txs))
	copy(ordered, txs)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].MinedAt.Before(ordered[j].MinedAt)
	})

	stored := 0
	var highWater *time.Time
	contiguous := true

	for _, dt := range ordered {
		ok := true

		raw, err := decodedTxToRawTx(w.ID, dt)
		if err != nil {
			c.logger.Warn("failed to serialize transaction, skipping",
				"wallet_id", w.ID,
				"chain", chain,
				"external_id", dt.ID,
				"error", err)
			ok = false
		} else if err := c.rawTxRepo.UpsertRawTransaction(ctx, raw); err != nil {
			c.logger.Error("failed to upsert raw transaction",
				"wallet_id", w.ID,
				"chain", chain,
				"external_id", dt.ID,
				"error", err)
			ok = false
		}

		if ok {
			stored++
		}

		// The first failure ends the contiguous prefix. Later successes are kept
		// (the upsert is idempotent, so re-fetching them next cycle is free) but
		// they must not drag the cursor past the gap.
		if !ok {
			if contiguous {
				c.logger.Warn("cursor halted at unstored transaction; tail will be re-fetched",
					"wallet_id", w.ID,
					"chain", chain,
					"external_id", dt.ID,
					"mined_at", dt.MinedAt)
			}
			contiguous = false
			continue
		}

		if contiguous {
			t := dt.MinedAt
			highWater = &t
		}
	}

	return stored, highWater, contiguous
}

// extractAssets iterates over decoded transactions and upserts asset metadata
// into the chain_assets table. Deduplicates by symbol:chain within the batch.
func (c *Collector) extractAssets(ctx context.Context, txs []DecodedTransaction) {
	if c.assetRepo == nil {
		return
	}

	type assetKey struct {
		symbol  string
		chainID string
	}
	seen := make(map[assetKey]bool)

	for _, dt := range txs {
		for _, t := range dt.Transfers {
			if t.AssetSymbol == "" {
				continue
			}
			key := assetKey{t.AssetSymbol, dt.ChainID}
			if seen[key] {
				continue
			}
			seen[key] = true

			if err := c.assetRepo.Upsert(ctx, &SyncAsset{
				Symbol:          t.AssetSymbol,
				Name:            t.AssetName,
				ChainID:         dt.ChainID,
				ContractAddress: t.ContractAddress,
				Decimals:        t.Decimals,
				IconURL:         t.IconURL,
			}); err != nil {
				c.logger.Warn("failed to upsert sync asset",
					"symbol", t.AssetSymbol,
					"chain_id", dt.ChainID,
					"error", err)
			}
		}

		if dt.Fee != nil && dt.Fee.AssetSymbol != "" {
			key := assetKey{dt.Fee.AssetSymbol, dt.ChainID}
			if !seen[key] {
				seen[key] = true
				if err := c.assetRepo.Upsert(ctx, &SyncAsset{
					Symbol:   dt.Fee.AssetSymbol,
					Name:     dt.Fee.AssetName,
					ChainID:  dt.ChainID,
					Decimals: dt.Fee.Decimals,
					IconURL:  dt.Fee.IconURL,
				}); err != nil {
					c.logger.Warn("failed to upsert sync asset (fee)",
						"symbol", dt.Fee.AssetSymbol,
						"chain_id", dt.ChainID,
						"error", err)
				}
			}
		}
	}
}

// decodedTxToRawTx converts a DecodedTransaction to a RawTransaction for storage
func decodedTxToRawTx(walletID uuid.UUID, dt DecodedTransaction) (*RawTransaction, error) {
	rawJSON, err := json.Marshal(dt)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal decoded transaction: %w", err)
	}

	return &RawTransaction{
		WalletID:         walletID,
		ExternalID:       dt.ID,
		TxHash:           dt.TxHash,
		ChainID:          dt.ChainID,
		OperationType:    string(dt.OperationType),
		MinedAt:          dt.MinedAt,
		Status:           dt.Status,
		RawJSON:          rawJSON,
		ProcessingStatus: ProcessingStatusPending,
	}, nil
}
