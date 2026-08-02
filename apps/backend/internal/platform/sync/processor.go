package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Processor handles Phase 3: processing raw transactions through the ledger.
//
// Every raw it books goes through the TxBuilder — there is no direct path to the
// ledger service. That is what guarantees each booked transaction passes the
// builder's price resolution and so can acquire a cost basis (issue #53).
type Processor struct {
	rawTxRepo  RawTransactionRepository
	walletRepo WalletRepository
	txBuilder  *TxBuilder
	logger     *logger.Logger
}

// NewProcessor creates a new Processor
func NewProcessor(
	rawTxRepo RawTransactionRepository,
	walletRepo WalletRepository,
	txBuilder *TxBuilder,
	log *logger.Logger,
) *Processor {
	return &Processor{
		rawTxRepo:  rawTxRepo,
		walletRepo: walletRepo,
		txBuilder:  txBuilder,
		logger:     log.WithField("component", "processor"),
	}
}

// ProcessAll processes all pending raw transactions for a wallet in
// chronological order, after first resolving which of them are two halves of one
// cross-chain bridge (issue #33).
//
// Stitching has to happen here, over the whole pending set, rather than per-raw:
// a bridge is one economic event decoded as two independent transactions on two
// chains, and the provider links them in neither direction. Only a join across
// the wallet's collected raws can pair them, and the pairing must be settled
// before either leg is booked — a realized disposal cannot be taken back.
func (p *Processor) ProcessAll(ctx context.Context, w *wallet.Wallet) error {
	if err := p.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseProcessing)); err != nil {
		return fmt.Errorf("failed to set sync phase: %w", err)
	}

	raws, err := p.rawTxRepo.GetPendingByWallet(ctx, w.ID)
	if err != nil {
		return fmt.Errorf("failed to get pending raw transactions: %w", err)
	}

	if len(raws) == 0 {
		p.logger.Info("no pending raw transactions", "wallet_id", w.ID)
		if err := p.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseSynced)); err != nil {
			return fmt.Errorf("failed to set sync phase: %w", err)
		}
		return nil
	}

	// Secondary sort: within same mined_at, use operationPriority (inflows before outflows)
	sort.SliceStable(raws, func(i, j int) bool {
		if !raws[i].MinedAt.Equal(raws[j].MinedAt) {
			return raws[i].MinedAt.Before(raws[j].MinedAt)
		}
		return operationPriority(OperationType(raws[i].OperationType)) < operationPriority(OperationType(raws[j].OperationType))
	})

	// Phase 2.5: bridge stitching (issue #33). This sits BETWEEN collect and the
	// per-raw processing below because a bridge is one economic event split
	// across two raws on two chains: the decision needs both in hand, and it has
	// to be made before either is booked. Once a source leg's disposal is
	// realized, nothing downstream can un-realize it.
	plan := p.planStitch(w, raws)

	p.logger.Info("processing raw transactions",
		"wallet_id", w.ID,
		"count", len(raws))

	var lastSuccessfulMinedAt *time.Time
	processed := 0
	skipped := 0
	deferred := 0
	errCount := 0
	consecutiveErrors := 0
	held := 0
	stitched := 0

	for i, raw := range raws {
		var ledgerTxID *uuid.UUID
		var processErr error

		switch plan.Decision(i) {
		case StitchHold:
			// A bridge leg whose counterpart has not been collected yet, still
			// inside the match window. Leave the raw PENDING and record nothing.
			//
			// On the send side, booking the transfer_out now would realize a
			// disposal that an arriving receive would force us to reverse
			// (ADR-0002's hold-don't-reverse rule). On the receive side, booking
			// the transfer_in now would mark the raw processed and drop it from
			// the pending set, so the send arriving next cycle would find
			// nothing to match — leaving a transfer_in plus a transfer_out,
			// which is the fabricated disposal in a different disguise.
			//
			// The asset is simply absent from the portfolio while in transit,
			// which the north star accepts.
			p.logger.Debug("bridge leg held pending its counterpart",
				"wallet_id", w.ID, "raw_id", raw.ID, "external_id", raw.ExternalID,
				"chain_id", raw.ChainID, "operation_type", raw.OperationType,
				"mined_at", raw.MinedAt)
			held++
			consecutiveErrors = 0
			continue

		case StitchSuppress:
			// The receive leg of a stitched bridge. The whole movement — the
			// source-chain outflow AND this destination-chain inflow — is
			// recorded once by the source leg's cross-chain internal_transfer,
			// so this raw records nothing of its own. It is marked skipped
			// rather than left pending so it does not stall the wallet forever.
			p.logger.Debug("bridge receive leg absorbed into stitched internal transfer",
				"wallet_id", w.ID, "raw_id", raw.ID, "external_id", raw.ExternalID,
				"chain_id", raw.ChainID)
			if err := p.rawTxRepo.MarkSkipped(ctx, raw.ID, "stitched into cross-chain internal transfer"); err != nil {
				p.logger.Error("failed to mark stitched receive leg skipped", "raw_id", raw.ID, "error", err)
			}
			skipped++
			consecutiveErrors = 0
			t := raw.MinedAt
			lastSuccessfulMinedAt = &t
			continue

		case StitchAsSource:
			stitched++
			ledgerTxID, processErr = p.processStitchedSource(ctx, w, raw, plan.DestinationChain(i), plan.NetAmount(i))

		case StitchNone:
			ledgerTxID, processErr = p.processRegular(ctx, w, raw)
		}

		if processErr != nil {
			if errors.Is(processErr, ErrSharedTxPending) {
				// This raw belongs to an on-chain event owned by another of the
				// user's wallets that has not recorded it yet (typically the
				// incoming side of an internal transfer whose source wallet
				// syncs later). Leave the raw pending and move on: marking it
				// skipped would strand one side of a real transfer forever, and
				// marking it an error would burn the consecutive-error budget on
				// an ordinary ordering race.
				//
				// On a wallet that has synced before, the counterpart has had a
				// cycle to appear, so a still-unresolved raw is no longer an
				// ordinary race — it may be a counterpart wallet that was
				// deleted or whose chain is not enabled, which would leave this
				// raw pending indefinitely. Surface that at WARN so the stall is
				// visible rather than silent. (Ageing such a raw out to a plain
				// transfer_in is the bridge window's job — issue #33.)
				if w.LastSyncAt != nil {
					p.logger.Warn("raw still deferred after a previous sync: counterpart wallet has not recorded this event",
						"wallet_id", w.ID, "raw_id", raw.ID, "external_id", raw.ExternalID,
						"tx_hash", raw.TxHash, "chain_id", raw.ChainID)
				} else {
					p.logger.Debug("raw deferred: shared transaction not recorded yet",
						"wallet_id", w.ID, "raw_id", raw.ID, "external_id", raw.ExternalID)
				}
				deferred++
				consecutiveErrors = 0
				continue
			}

			if isDuplicateError(processErr) {
				// Idempotent — already processed
				if err := p.rawTxRepo.MarkSkipped(ctx, raw.ID, "duplicate"); err != nil {
					p.logger.Error("failed to mark duplicate as skipped", "raw_id", raw.ID, "error", err)
				}
				skipped++
				consecutiveErrors = 0
				t := raw.MinedAt
				lastSuccessfulMinedAt = &t
				continue
			}

			p.logger.Error("failed to process raw transaction",
				"wallet_id", w.ID,
				"raw_id", raw.ID,
				"external_id", raw.ExternalID,
				"error", processErr)

			if err := p.rawTxRepo.MarkError(ctx, raw.ID, processErr.Error()); err != nil {
				p.logger.Error("failed to mark error", "raw_id", raw.ID, "error", err)
			}

			errCount++
			consecutiveErrors++

			if consecutiveErrors > 5 {
				p.logger.Warn("too many consecutive errors, stopping processing",
					"wallet_id", w.ID,
					"consecutive_errors", consecutiveErrors)
				break
			}
			continue
		}

		// Success
		if ledgerTxID != nil {
			// Either this wallet recorded the transaction, or it observed an
			// on-chain event another of the user's wallets recorded. Both are
			// marked processed against the ledger transaction: the reference is
			// what lets the wipe reach a shared transaction from either side.
			if err := p.rawTxRepo.MarkProcessed(ctx, raw.ID, *ledgerTxID); err != nil {
				p.logger.Error("failed to mark processed", "raw_id", raw.ID, "error", err)
			}
		} else {
			// Nothing to record (failed, unclassifiable, or an intentionally
			// ignored operation such as approve).
			if err := p.rawTxRepo.MarkSkipped(ctx, raw.ID, "skipped by processor"); err != nil {
				p.logger.Error("failed to mark skipped", "raw_id", raw.ID, "error", err)
			}
			skipped++
		}

		consecutiveErrors = 0
		t := raw.MinedAt
		lastSuccessfulMinedAt = &t
		processed++
	}

	// Update last_sync_at cursor
	if lastSuccessfulMinedAt != nil {
		if err := p.walletRepo.SetSyncCompletedAt(ctx, w.ID, *lastSuccessfulMinedAt); err != nil {
			return fmt.Errorf("failed to update sync cursor: %w", err)
		}
	}

	if err := p.walletRepo.SetSyncPhase(ctx, w.ID, string(SyncPhaseSynced)); err != nil {
		return fmt.Errorf("failed to set sync phase: %w", err)
	}

	// Clear address cache after processing
	p.txBuilder.ClearCache()

	p.logger.Info("processing complete",
		"wallet_id", w.ID,
		"processed", processed,
		"skipped", skipped,
		"deferred", deferred,
		"held", held,
		"stitched", stitched,
		"errors", errCount)

	return nil
}

// planStitch decodes the wallet's pending raws and asks the stitcher which of
// them are two halves of one cross-chain bridge (issue #33).
//
// Both legs of a self-bridge belong to the SAME wallet row — a wallet is one
// address across every chain in its chain set — so the wallet's own pending raws
// are exactly the right join scope, and no cross-wallet query is needed.
//
// A raw that fails to decode is simply left out of the plan: it will fail again
// in the main loop and be marked errored there, with the real error message.
// Nothing is stitched on the strength of a transaction we could not read.
func (p *Processor) planStitch(w *wallet.Wallet, raws []*RawTransaction) StitchPlan {
	decoded := make([]DecodedTransaction, len(raws))
	for i, raw := range raws {
		if err := json.Unmarshal(raw.RawJSON, &decoded[i]); err != nil {
			p.logger.Warn("failed to decode raw for bridge stitching, leaving it unstitched",
				"wallet_id", w.ID, "raw_id", raw.ID, "error", err)
			decoded[i] = DecodedTransaction{}
		}
	}

	plan := Stitch(decoded, w.Address, time.Now().UTC())

	if len(plan.Decisions) > 0 {
		p.logger.Info("bridge stitch plan derived",
			"wallet_id", w.ID, "decisions", len(plan.Decisions))
	}
	return plan
}

// processStitchedSource records a matched bridge send leg as a CROSS-CHAIN
// internal transfer instead of a transfer_out.
//
// This is the whole point of the ticket. As a transfer_out the leg would dispose
// of the lot — realizing PnL on a move between the user's own chains and leaving
// the destination to open a fresh lot at market price, resetting the cost basis.
// Recorded as an internal transfer spanning source→destination chain, the
// TaxLotHook's existing carry-over path links the new lot to the consumed one
// and carries the basis across with no PnL realized.
//
// Both wallet ids are this wallet: a self-bridge moves the user's funds between
// chains at the same address, which the internal-transfer model permits
// precisely because the two legs are on different chains (#32).
func (p *Processor) processStitchedSource(
	ctx context.Context,
	w *wallet.Wallet,
	raw *RawTransaction,
	destChain string,
	netAmount *big.Int,
) (*uuid.UUID, error) {
	var dt DecodedTransaction
	if err := json.Unmarshal(raw.RawJSON, &dt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stitched bridge leg: %w", err)
	}

	if destChain == "" || destChain == dt.ChainID {
		// Defensive: a stitch decision without a distinct destination chain is
		// not a bridge. Fall back to ordinary processing rather than emitting an
		// internal transfer that claims to cross a boundary it does not.
		p.logger.Warn("stitched bridge leg has no distinct destination chain, processing normally",
			"wallet_id", w.ID, "raw_id", raw.ID, "chain_id", dt.ChainID, "dest_chain", destChain)
		return p.txBuilder.ProcessTransaction(ctx, w, dt)
	}

	dt.DestChainID = destChain

	p.logger.Info("recording stitched cross-chain internal transfer",
		"wallet_id", w.ID,
		"external_id", dt.ID,
		"source_chain", dt.ChainID,
		"dest_chain", destChain)

	return p.txBuilder.ProcessStitchedBridge(ctx, w, dt, netAmount)
}

// processRegular processes a raw transaction via TxBuilder
func (p *Processor) processRegular(ctx context.Context, w *wallet.Wallet, raw *RawTransaction) (*uuid.UUID, error) {
	var dt DecodedTransaction
	if err := json.Unmarshal(raw.RawJSON, &dt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal raw tx: %w", err)
	}

	ledgerTxID, err := p.txBuilder.ProcessTransaction(ctx, w, dt)
	if err != nil {
		return nil, err
	}

	return ledgerTxID, nil
}
