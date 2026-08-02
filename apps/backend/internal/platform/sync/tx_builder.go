package sync

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/asset"
	"github.com/kislikjeka/moontrack/internal/platform/lpposition"
	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// AssetUpserter finds-or-creates an asset by on-chain identity.
type AssetUpserter interface {
	UpsertByOnChainIdentity(ctx context.Context, chainID, contractAddress, symbol, name string, decimals int) (*asset.Asset, bool, error)
}

// JobEnqueuer enqueues a price backfill job for a given asset and target time.
type JobEnqueuer interface {
	Enqueue(ctx context.Context, assetID uuid.UUID, targetTime time.Time) (*price.BackfillJob, error)
}

// TxBuilder handles decoded transaction classification and ledger recording
// for transactions fetched via the sync provider.
type TxBuilder struct {
	walletRepo         WalletRepository
	ledgerSvc          LedgerService
	lpPositionSvc      LPPositionService
	lendingPositionSvc LendingPositionService
	assetUpsert        AssetUpserter
	jobEnqueuer        JobEnqueuer
	assetRegistry      AssetRegistry
	classifier         *Classifier
	logger             *logger.Logger
	addressCache       map[string][]uuid.UUID
}

// NewTxBuilder creates a new TxBuilder.
//
// assetRegistry may be nil, in which case identity resolution is skipped and
// the builder behaves exactly as before the registry existed. Tests that do not
// exercise identity rely on that.
func NewTxBuilder(walletRepo WalletRepository, ledgerSvc LedgerService, lpPositionSvc LPPositionService, lendingPositionSvc LendingPositionService, log *logger.Logger, assetUpsert AssetUpserter, jobEnqueuer JobEnqueuer, assetRegistry AssetRegistry) *TxBuilder {
	return &TxBuilder{
		walletRepo:         walletRepo,
		ledgerSvc:          ledgerSvc,
		lpPositionSvc:      lpPositionSvc,
		lendingPositionSvc: lendingPositionSvc,
		assetUpsert:        assetUpsert,
		jobEnqueuer:        jobEnqueuer,
		assetRegistry:      assetRegistry,
		classifier:         NewClassifier(),
		logger:             log.WithField("component", "tx_builder"),
		addressCache:       make(map[string][]uuid.UUID),
	}
}

// resolveAssetIdentities resolves every leg of a decoded transaction to its
// registry UUID (issue #56).
//
// It sits here, at the one point where the whole transaction is in hand, rather
// than inside the per-type raw-data builders. Spreading it across the builders
// would make "every leg is resolved" a property of fifteen switch arms that
// each have to remember; here it is a property of the one loop they all pass
// through, and a new transaction type cannot forget it.
//
// EVERY leg means every leg, natives included. The old on-chain upsert ran only
// for the price-backfill job and returned early on an empty contract, so the
// native coin — the largest position in most wallets — never acquired an
// on-chain identity at all. Under (chain, contract) identity a native leg is
// just the leg whose contract is the sentinel.
//
// The fee leg is resolved too. A fee is always paid in the native coin, which
// is exactly the case the old path skipped, so gas would otherwise be the one
// movement with no resolvable asset.
//
// Failures are logged and swallowed. In the expand phase the ledger still runs
// on symbolic asset_ids and nothing downstream reads these UUIDs yet, so a
// registry error must not cost the transaction — dropping it would trade a
// cosmetic gap for the one thing the product will not accept, a lost movement.
// That balance flips in the contract phase (#59), when the ledger depends on
// the resolved identity and a failure has to become fatal.
func (p *TxBuilder) resolveAssetIdentities(ctx context.Context, tx DecodedTransaction) {
	if p.assetRegistry == nil {
		return
	}

	// Dedupe within the transaction: a swap routed through several hops emits
	// the same asset repeatedly, and one resolve per identity is enough.
	seen := make(map[AssetKey]bool, len(tx.Transfers)+1)

	resolve := func(chain, contract, symbol, name string, decimals int) {
		key := NewAssetKey(chain, contract)
		if !key.Valid() {
			// NewAssetKey turns a missing contract into the sentinel, so the
			// only way to be here is a missing CHAIN — a provider defect, not a
			// native leg. Logged rather than dropped in silence: an asset that
			// never reaches the registry is exactly the invisible gap this file
			// makes a point of surfacing everywhere else.
			p.logger.Warn("asset identity skipped: incomplete on-chain identity",
				"chain", price.SanitizeLogField(key.Chain),
				"contract", price.SanitizeLogField(key.Contract),
				"asset_symbol", price.SanitizeLogField(symbol),
			)
			return
		}
		if seen[key] {
			return
		}
		seen[key] = true

		if name == "" {
			name = symbol
		}
		if _, err := p.assetRegistry.Resolve(ctx, key, symbol, name, decimals); err != nil {
			p.logger.Warn("asset identity resolve failed during sync",
				"chain", price.SanitizeLogField(key.Chain),
				"contract", price.SanitizeLogField(key.Contract),
				"asset_symbol", price.SanitizeLogField(symbol),
				"error", price.SanitizeLogField(err.Error()),
			)
		}
	}

	for i := range tx.Transfers {
		t := &tx.Transfers[i]
		// The destination chain of a stitched bridge is where the inbound leg
		// lands, so that leg's identity belongs to that chain, not to the chain
		// the transaction was observed on. Getting this wrong would file the
		// arriving asset under the source chain and defeat the point of the
		// composite key.
		chain := tx.ChainID
		if tx.DestChainID != "" && t.Direction == DirectionIn {
			chain = tx.DestChainID
		}
		resolve(chain, t.ContractAddress, t.AssetSymbol, t.AssetName, t.Decimals)
	}

	if tx.Fee != nil && tx.Fee.AssetSymbol != "" {
		// DecodedFee carries no contract: gas is paid in the native coin by
		// construction, so the sentinel is not a guess here.
		resolve(tx.ChainID, NativeContract, tx.Fee.AssetSymbol, tx.Fee.AssetName, tx.Fee.Decimals)
	}
}

// ProcessTransaction classifies a decoded transaction and records it to the ledger.
// Returns the ledger transaction ID on success, nil if skipped, or an error.
func (p *TxBuilder) ProcessTransaction(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) (*uuid.UUID, error) {
	if tx.Status == "failed" {
		p.logger.Debug("skipping failed transaction", "tx_hash", tx.TxHash)
		return nil, nil
	}

	p.reportUnknownLegActions(tx)

	txType := p.classifier.Classify(tx)
	p.logger.Debug("transaction classified", "tx_hash", tx.TxHash, "op_type", tx.OperationType, "tx_type", string(txType))

	if txType == "" {
		p.logger.Debug("skipping unclassifiable transaction", "tx_hash", tx.TxHash, "op_type", tx.OperationType)
		return nil, nil
	}

	// Resolve every leg's on-chain identity before anything is built or
	// recorded. Deliberately ahead of the internal-transfer skip below: the
	// incoming side records no ledger transaction, but the assets it saw are
	// real and the registry is a catalogue of assets, not of bookkeeping.
	p.resolveAssetIdentities(ctx, tx)

	txType, destWalletID := p.detectInternalTransfer(ctx, w, tx, txType)

	// The incoming side of an internal transfer does not record anything: the
	// movement is one event, owned by the outgoing (source) side. It still has
	// to come away pointing at that shared transaction, though — the raw's
	// ledger_tx_id is what ties this wallet to the transaction that credited
	// it, and wipe/replay is scoped by exactly that reference (issue #31).
	if txType == ledger.TxTypeInternalTransfer && p.isIncomingSide(w, tx) {
		p.logger.Debug("skipping internal transfer (recorded from source side)",
			"wallet_id", w.ID, "tx_hash", tx.TxHash, "external_id", tx.ID)
		return p.findSharedLedgerTx(ctx, tx.ID)
	}

	var data map[string]interface{}
	externalID := tx.ID

	switch txType {
	case ledger.TxTypeTransferIn:
		data = p.buildTransferInData(ctx, w, tx)
	case ledger.TxTypeTransferOut:
		data = p.buildTransferOutData(ctx, w, tx)
	case ledger.TxTypeSwap:
		data = p.buildSwapData(ctx, w, tx)
	case ledger.TxTypeInternalTransfer:
		data = p.buildInternalTransferData(w, tx, destWalletID)
	case ledger.TxTypeDefiDeposit:
		data = p.buildDeFiDepositData(ctx, w, tx)
	case ledger.TxTypeDefiWithdraw:
		data = p.buildDeFiWithdrawData(ctx, w, tx)
	case ledger.TxTypeDefiClaim:
		data = p.buildDeFiClaimData(ctx, w, tx)
	case ledger.TxTypeLPDeposit:
		data = p.buildLPDepositData(ctx, w, tx)
	case ledger.TxTypeLPWithdraw:
		data = p.buildLPWithdrawData(ctx, w, tx)
	case ledger.TxTypeLPClaimFees:
		data = p.buildLPClaimFeesData(ctx, w, tx)
	case ledger.TxTypeLendingSupply:
		data = p.buildLendingSupplyData(w, tx)
	case ledger.TxTypeLendingWithdraw:
		data = p.buildLendingWithdrawData(w, tx)
	case ledger.TxTypeLendingBorrow:
		data = p.buildLendingBorrowData(w, tx)
	case ledger.TxTypeLendingRepay:
		data = p.buildLendingRepayData(w, tx)
	case ledger.TxTypeLendingClaim:
		data = p.buildLendingClaimData(w, tx)
	default:
		p.logger.Warn("unhandled transaction type", "type", txType, "tx_hash", tx.TxHash)
		return nil, nil
	}

	p.tagUnclassifiedForReview(tx, txType, data)

	ledgerTx, err := p.ledgerSvc.RecordTransaction(ctx, txType, sourceName, &externalID, tx.MinedAt, data)
	if err != nil {
		if isDuplicateError(err) {
			// Another of the user's wallets already recorded this on-chain
			// event: external_id is chain:txHash under UNIQUE(source,
			// external_id), so one event is one ledger transaction however many
			// wallets observed it. This wallet yields ownership but still
			// reports the shared transaction, so its raw references it.
			p.logger.Debug("transaction already recorded by another wallet (idempotent)",
				"external_id", externalID, "wallet_id", w.ID)
			return p.findSharedLedgerTx(ctx, externalID)
		}
		return nil, fmt.Errorf("failed to record transaction: %w", err)
	}

	p.logger.Debug("transaction recorded to ledger", "tx_hash", tx.TxHash, "tx_type", string(txType), "external_id", externalID)

	// Post-process LP transactions: update LP position aggregates
	if p.lpPositionSvc != nil {
		switch txType {
		case ledger.TxTypeLPDeposit:
			p.handleLPDeposit(ctx, w, tx)
		case ledger.TxTypeLPWithdraw:
			p.handleLPWithdraw(ctx, w, tx)
		case ledger.TxTypeLPClaimFees:
			p.handleLPClaimFees(ctx, w, tx)
		}
	}

	// Post-process lending transactions: update lending position aggregates
	if p.lendingPositionSvc != nil {
		switch txType {
		case ledger.TxTypeLendingSupply:
			p.handleLendingSupply(ctx, w, tx)
		case ledger.TxTypeLendingWithdraw:
			p.handleLendingWithdraw(ctx, w, tx)
		case ledger.TxTypeLendingBorrow:
			p.handleLendingBorrow(ctx, w, tx)
		case ledger.TxTypeLendingRepay:
			p.handleLendingRepay(ctx, w, tx)
		case ledger.TxTypeLendingClaim:
			p.handleLendingClaim(ctx, w, tx)
		}
	}

	return &ledgerTx.ID, nil
}

// ProcessStitchedBridge records a bridge send leg that the stitcher matched to a
// receive leg on another chain, as ONE cross-chain internal_transfer (issue #33,
// ADR-0002).
//
// It deliberately does NOT go through ProcessTransaction's classify-then-detect
// path, because neither step can reach the right answer here. The classifier
// sees an outbound-only transaction and says transfer_out; detectInternalTransfer
// then looks for a counterparty address belonging to the user and finds the
// BRIDGE CONTRACT (or the null address), because that is genuinely who the funds
// were sent to on-chain. The knowledge that those funds came back to the user on
// another chain exists only in the stitcher's cross-leg match, so the type is
// asserted here rather than re-derived.
//
// tx.DestChainID must already be set to the destination chain; the caller
// establishes that from the matching receive leg.
//
// netAmount is the quantity the stitcher matched on — everything of the asset
// that left the wallet, minus anything of it refunded in the same transaction.
// It is passed in rather than re-derived here because the matcher and the ledger
// must agree on one number: if they diverge, the destination lot opens at a
// quantity that never arrived while the source is credited a quantity that never
// left. The transaction would still balance, so nothing downstream would notice.
func (p *TxBuilder) ProcessStitchedBridge(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction, netAmount *big.Int) (*uuid.UUID, error) {
	if tx.Status == "failed" {
		p.logger.Debug("skipping failed stitched bridge leg", "tx_hash", tx.TxHash)
		return nil, nil
	}

	// A stitched bridge is the one transaction that spans two chains, so its
	// legs resolve against two different chains — handled inside the resolver,
	// which reads DestChainID for the inbound leg.
	p.resolveAssetIdentities(ctx, tx)

	// Source and destination wallet are the same row: one address, two chains.
	// The internal-transfer model allows that exactly when the chains differ.
	data := p.buildInternalTransferData(w, tx, &w.ID)
	if netAmount != nil && netAmount.Sign() > 0 {
		data["amount"] = money.NewBigInt(netAmount).String()
	}
	externalID := tx.ID

	ledgerTx, err := p.ledgerSvc.RecordTransaction(ctx, ledger.TxTypeInternalTransfer, sourceName, &externalID, tx.MinedAt, data)
	if err != nil {
		if isDuplicateError(err) {
			p.logger.Debug("stitched bridge already recorded (idempotent)",
				"external_id", externalID, "wallet_id", w.ID)
			return p.findSharedLedgerTx(ctx, externalID)
		}
		return nil, fmt.Errorf("failed to record stitched bridge transfer: %w", err)
	}

	p.logger.Debug("stitched bridge recorded as cross-chain internal transfer",
		"tx_hash", tx.TxHash, "source_chain", tx.ChainID, "dest_chain", tx.DestChainID)

	return &ledgerTx.ID, nil
}

// The review tag on a recorded transaction whose classification rests on a
// shape the provider could not identify. It rides on the ledger transaction's
// raw_data (JSONB), so the audit trail outlives any log retention window.
const (
	unclassifiedReviewKey       = "unclassified_review"
	unclassifiedReviewReasonKey = "unclassified_review_reason"
	unclassifiedProviderTypeKey = "unclassified_provider_type"
)

// reportUnknownLegActions logs every leg action the provider stamped that
// MoonTrack's closed vocabulary does not recognize (issue #57).
//
// This is the audit surface for the one limitation the receipt rule accepts.
// The vocabulary of leg actions belongs to the provider, not to us, and is not
// frozen: a lending protocol that mints its receipt under an action we have
// never seen will have that receipt treated as principal and booked as a
// position, double-counting the supply exactly as before the rule existed.
// Nothing in the data distinguishes that case from a genuine new principal
// action, so it cannot be decided automatically — which is precisely why it
// must not pass in silence. A WARN turns an invisible mis-booking into a
// searchable line naming the action, so the vocabulary can be extended from
// evidence.
//
// The unrecognized leg is treated as PRINCIPAL, never dropped. Guessing the
// other way would silently delete real movements on every protocol whose
// vocabulary we have not yet met, and a lost movement is the one outcome the
// product does not accept; an over-counted position is visible and repairable
// by re-sync, a deleted one is neither.
func (p *TxBuilder) reportUnknownLegActions(tx DecodedTransaction) {
	for _, action := range tx.LegActions {
		if !IsUnknownLegAction(action) {
			continue
		}
		p.logger.Warn("unrecognized provider leg action treated as principal",
			"tx_hash", price.SanitizeLogField(tx.TxHash),
			"chain_id", price.SanitizeLogField(tx.ChainID),
			"external_id", price.SanitizeLogField(tx.ID),
			"leg_action", price.SanitizeLogField(action),
			"provider_type", price.SanitizeLogField(tx.ProviderType),
		)
	}
}

// tagUnclassifiedForReview makes the genuinely risky unclassified shape
// observable, mutating data to carry the tag (issue #30).
//
// A transaction the provider could not classify still carries transfers, so it
// is routed through the classifier rather than dropped: one-directional cases
// are unambiguous — inflow only means value came in, outflow only means value
// left. But when an unclassified transaction moves value BOTH ways, whatever
// type it lands on asserts a relationship between the two legs that nobody
// actually established: booked as a swap it realizes disposal PnL, and on an
// unknown DeFi shape (a lending action, an LP move, a bridge leg the provider
// failed to decode) that PnL is phantom.
//
// The condition is both-direction-and-unclassified, deliberately NOT "was
// booked as a swap". Classify consults the per-leg protocol actions before the
// in/out fallback, so an unclassified transaction whose legs carry a lending or
// liquidity action books as lending_supply or an LP type instead — a shape the
// provider could not name can still land there, since the action says which
// market it touched but not what it did. Keying on the resulting type would let
// exactly those escape the audit trail, which is the outcome this exists to
// prevent.
//
// The judgment call is deferred rather than guessed: the transaction IS
// recorded, so no data is lost, and the classification is left alone — this
// only marks the risk so the bucket can be measured against real data before
// any bespoke unclassified handling is built. A provider-classified
// transaction is never tagged; flagging those would drown the signal.
func (p *TxBuilder) tagUnclassifiedForReview(tx DecodedTransaction, txType ledger.TransactionType, data map[string]interface{}) {
	hasIn, hasOut := directions(tx.Transfers)
	if !tx.Unclassified || !hasIn || !hasOut {
		return
	}

	protocol := tx.Protocol
	if protocol == "" {
		protocol = "unknown"
	}
	providerType := tx.ProviderType
	if providerType == "" {
		providerType = "unknown"
	}
	reason := fmt.Sprintf(
		"provider could not classify this transaction (provider type %q); both inflow and outflow present, booked as %s on protocol hint %q — may realize phantom PnL",
		providerType, txType, protocol,
	)

	p.logger.Warn("unclassified transaction with both inflow and outflow recorded for review",
		"tx_hash", tx.TxHash,
		"chain_id", tx.ChainID,
		"external_id", tx.ID,
		"protocol", protocol,
		"provider_type", providerType,
		"tx_type", string(txType),
		"reason", reason)

	data[unclassifiedReviewKey] = true
	data[unclassifiedReviewReasonKey] = reason
	data[unclassifiedProviderTypeKey] = providerType
}

// findSharedLedgerTx resolves the ledger transaction an on-chain event already
// became, for a wallet that observed the event but does not own the recording.
//
// Returning the id (rather than nil) is what lets the caller mark this wallet's
// raw as referencing the shared transaction. Without that reference the wipe
// cannot reach the transaction from this side: `wipe_wallet_ledger` scopes
// itself to the transactions a wallet's raws point at, so a dangling NULL would
// leave one participant unable to re-derive a transaction it takes part in.
//
// A nil result is legitimate and means "not recorded yet" — the counterpart
// wallet has not synced this event. The raw stays pending and a later cycle
// resolves it. A lookup *error*, by contrast, is propagated: silently dropping
// the reference would mark the raw done while leaving it orphaned.
func (p *TxBuilder) findSharedLedgerTx(ctx context.Context, externalID string) (*uuid.UUID, error) {
	existing, err := p.ledgerSvc.FindBySourceExternalID(ctx, sourceName, externalID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve shared ledger transaction %q: %w", externalID, err)
	}
	if existing == nil {
		p.logger.Debug("shared transaction not recorded yet, leaving raw pending",
			"external_id", externalID)
		return nil, fmt.Errorf("%w: %s", ErrSharedTxPending, externalID)
	}
	return &existing.ID, nil
}

// detectInternalTransfer checks if a transfer_in/transfer_out is actually an internal
// transfer between user wallets.
func (p *TxBuilder) detectInternalTransfer(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction, txType ledger.TransactionType) (ledger.TransactionType, *uuid.UUID) {
	if txType != ledger.TxTypeTransferIn && txType != ledger.TxTypeTransferOut {
		return txType, nil
	}

	for _, t := range tx.Transfers {
		var counterpartyAddr string
		if t.Direction == DirectionIn {
			counterpartyAddr = t.Sender
		} else {
			counterpartyAddr = t.Recipient
		}

		// Bridge guard: if counterparty is the same address as the wallet,
		// it's a bridge (same address, different chain), not an internal transfer
		if strings.EqualFold(counterpartyAddr, w.Address) {
			continue
		}

		if p.isUserWallet(ctx, counterpartyAddr, w.UserID) {
			destID := p.getWalletByAddress(ctx, counterpartyAddr, w.UserID)
			if destID != nil {
				p.logger.Debug("internal transfer detected", "tx_hash", tx.TxHash, "source_wallet", w.ID, "dest_wallet", *destID)
			}
			return ledger.TxTypeInternalTransfer, destID
		}
	}

	return txType, nil
}

// isIncomingSide checks if the wallet is on the receiving side of transfers.
func (p *TxBuilder) isIncomingSide(w *wallet.Wallet, tx DecodedTransaction) bool {
	walletAddr := strings.ToLower(w.Address)
	for _, t := range tx.Transfers {
		if t.Direction == DirectionIn && strings.ToLower(t.Recipient) == walletAddr {
			return true
		}
	}
	return false
}

// isUserWallet checks if an address belongs to any of the user's wallets.
func (p *TxBuilder) isUserWallet(ctx context.Context, address string, userID uuid.UUID) bool {
	address = strings.ToLower(address)
	if _, ok := p.addressCache[address]; ok {
		return true
	}
	wallets, err := p.walletRepo.GetWalletsByAddressAndUserID(ctx, address, userID)
	if err != nil {
		p.logger.Error("failed to check wallet ownership", "address", address, "error", err)
		return false
	}
	if len(wallets) > 0 {
		ids := make([]uuid.UUID, len(wallets))
		for i, w := range wallets {
			ids[i] = w.ID
		}
		p.addressCache[address] = ids
		return true
	}
	return false
}

// getWalletByAddress returns the wallet ID for an address belonging to a specific user.
func (p *TxBuilder) getWalletByAddress(ctx context.Context, address string, userID uuid.UUID) *uuid.UUID {
	address = strings.ToLower(address)
	wallets, err := p.walletRepo.GetWalletsByAddressAndUserID(ctx, address, userID)
	if err != nil || len(wallets) == 0 {
		return nil
	}
	return &wallets[0].ID
}

// ClearCache clears the address cache.
func (p *TxBuilder) ClearCache() {
	p.addressCache = make(map[string][]uuid.UUID)
}

// --- LP position post-processing ---

func (p *TxBuilder) handleLPDeposit(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	token0, token1 := p.extractTokenPair(tx.Transfers, DirectionOut)
	if token0.AssetSymbol == "" {
		p.logger.Warn("LP deposit: no outgoing transfers for token pair extraction", "tx_hash", tx.TxHash)
		return
	}

	chainID := tx.ChainID
	pos, err := p.lpPositionSvc.FindOrCreate(ctx, w.UserID, w.ID, chainID, tx.Protocol, tx.NFTTokenID, "",
		lpposition.TokenInfo{Symbol: token0.AssetSymbol, Contract: legacyContractAddress(token0.ContractAddress), Decimals: token0.Decimals},
		lpposition.TokenInfo{Symbol: token1.AssetSymbol, Contract: legacyContractAddress(token1.ContractAddress), Decimals: token1.Decimals},
		tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("LP deposit: failed to find or create position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	token0Amt, token1Amt, usdValue := p.calcLPAmounts(tx.Transfers, DirectionOut, pos)
	if err := p.lpPositionSvc.RecordDeposit(ctx, pos.ID, token0Amt, token1Amt, usdValue); err != nil {
		p.logger.Error("LP deposit: failed to record deposit", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *TxBuilder) handleLPWithdraw(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	token0, token1 := p.extractTokenPair(tx.Transfers, DirectionIn)
	if token0.AssetSymbol == "" {
		p.logger.Warn("LP withdraw: no incoming transfers for token pair extraction", "tx_hash", tx.TxHash)
		return
	}

	chainID := tx.ChainID

	// Try to find position by NFT token ID first, then by token pair
	var pos *lpposition.LPPosition
	var err error
	if tx.NFTTokenID != "" {
		pos, err = p.lpPositionSvc.FindOrCreate(ctx, w.UserID, w.ID, chainID, tx.Protocol, tx.NFTTokenID, "",
			lpposition.TokenInfo{Symbol: token0.AssetSymbol, Contract: legacyContractAddress(token0.ContractAddress), Decimals: token0.Decimals},
			lpposition.TokenInfo{Symbol: token1.AssetSymbol, Contract: legacyContractAddress(token1.ContractAddress), Decimals: token1.Decimals},
			tx.MinedAt,
		)
	} else {
		pos, err = p.lpPositionSvc.FindOpenByTokenPair(ctx, w.ID, chainID, tx.Protocol, token0.AssetSymbol, token1.AssetSymbol)
	}
	if err != nil {
		p.logger.Error("LP withdraw: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}
	if pos == nil {
		p.logger.Warn("LP withdraw: no open position found", "tx_hash", tx.TxHash, "token0", token0.AssetSymbol, "token1", token1.AssetSymbol)
		return
	}

	token0Amt, token1Amt, usdValue := p.calcLPAmounts(tx.Transfers, DirectionIn, pos)
	if err := p.lpPositionSvc.RecordWithdraw(ctx, pos.ID, token0Amt, token1Amt, usdValue); err != nil {
		p.logger.Error("LP withdraw: failed to record withdraw", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *TxBuilder) handleLPClaimFees(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	token0, token1 := p.extractTokenPair(tx.Transfers, DirectionIn)
	if token0.AssetSymbol == "" {
		p.logger.Warn("LP claim fees: no incoming transfers for token pair extraction", "tx_hash", tx.TxHash)
		return
	}

	chainID := tx.ChainID

	var pos *lpposition.LPPosition
	var err error
	if tx.NFTTokenID != "" {
		pos, err = p.lpPositionSvc.FindOrCreate(ctx, w.UserID, w.ID, chainID, tx.Protocol, tx.NFTTokenID, "",
			lpposition.TokenInfo{Symbol: token0.AssetSymbol, Contract: legacyContractAddress(token0.ContractAddress), Decimals: token0.Decimals},
			lpposition.TokenInfo{Symbol: token1.AssetSymbol, Contract: legacyContractAddress(token1.ContractAddress), Decimals: token1.Decimals},
			tx.MinedAt,
		)
	} else {
		pos, err = p.lpPositionSvc.FindOpenByTokenPair(ctx, w.ID, chainID, tx.Protocol, token0.AssetSymbol, token1.AssetSymbol)
	}
	if err != nil {
		p.logger.Error("LP claim fees: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}
	if pos == nil {
		p.logger.Warn("LP claim fees: no open position found", "tx_hash", tx.TxHash, "token0", token0.AssetSymbol, "token1", token1.AssetSymbol)
		return
	}

	token0Amt, token1Amt, usdValue := p.calcLPAmounts(tx.Transfers, DirectionIn, pos)
	if err := p.lpPositionSvc.RecordClaimFees(ctx, pos.ID, token0Amt, token1Amt, usdValue); err != nil {
		p.logger.Error("LP claim fees: failed to record claim", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

// extractTokenPair extracts token0 and token1 from transfers matching the given direction.
// Returns up to two unique tokens. If only one token, token1 is returned with empty symbol.
func (p *TxBuilder) extractTokenPair(transfers []DecodedTransfer, dir TransferDirection) (DecodedTransfer, DecodedTransfer) {
	var token0, token1 DecodedTransfer
	seen := make(map[string]bool)
	for _, t := range transfers {
		if t.Direction != dir {
			continue
		}
		if !seen[t.AssetSymbol] {
			seen[t.AssetSymbol] = true
			if token0.AssetSymbol == "" {
				token0 = t
			} else {
				token1 = t
				break
			}
		}
	}
	return token0, token1
}

// calcLPAmounts calculates token0/token1 amounts and total USD value from transfers
// matching a given direction, mapped to the position's token pair.
func (p *TxBuilder) calcLPAmounts(transfers []DecodedTransfer, dir TransferDirection, pos *lpposition.LPPosition) (*big.Int, *big.Int, *big.Int) {
	token0Amt := big.NewInt(0)
	token1Amt := big.NewInt(0)
	usdValue := big.NewInt(0)

	for _, t := range transfers {
		if t.Direction != dir {
			continue
		}
		switch t.AssetSymbol {
		case pos.Token0Symbol:
			token0Amt.Add(token0Amt, t.Amount)
		case pos.Token1Symbol:
			token1Amt.Add(token1Amt, t.Amount)
		}
		if t.USDPrice != nil && t.Amount != nil {
			v := money.CalcUSDValue(t.Amount, t.USDPrice, t.Decimals)
			usdValue.Add(usdValue, v)
		}
	}

	return token0Amt, token1Amt, usdValue
}

// --- Raw data builders ---

func (p *TxBuilder) buildTransferInData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["unique_id"] = tx.ID

	// Collect ALL incoming transfers. The provider can emit multiple transfers of
	// different assets in a single on-chain tx (e.g. Aave borrow returns
	// a debt receipt token + the real borrowed asset). Prior to this change
	// only the first matching transfer was kept, silently dropping the rest.
	transfers := make([]map[string]interface{}, 0, len(tx.Transfers))
	var first *DecodedTransfer
	for i := range tx.Transfers {
		t := &tx.Transfers[i]
		if t.Direction != DirectionIn {
			continue
		}
		transfers = append(transfers, p.buildTransferEntry(t))
		// Enqueue backfill jobs for EVERY unpriced transfer, not just the first.
		if t.USDPrice == nil {
			p.ensureBackfillJob(ctx, t, tx.ChainID, tx.MinedAt)
		}
		if first == nil {
			first = t
		}
	}
	// Fallback: no IN transfers found — use the first transfer of any direction
	// so legacy callers that inspect flat fields still see something.
	if first == nil && len(tx.Transfers) > 0 {
		first = &tx.Transfers[0]
		transfers = append(transfers, p.buildTransferEntry(first))
		if first.USDPrice == nil {
			p.ensureBackfillJob(ctx, first, tx.ChainID, tx.MinedAt)
		}
	}

	data["transfers"] = transfers

	// Legacy flat fields, populated from the first transfer for backwards
	// compatibility with consumers that read raw_transactions without
	// understanding the new shape.
	if first != nil {
		data["asset_id"] = first.AssetSymbol
		data["amount"] = money.NewBigInt(first.Amount).String()
		data["decimals"] = first.Decimals
		data["contract_address"] = legacyContractAddress(first.ContractAddress)
		data["from_address"] = first.Sender
		if first.USDPrice != nil {
			data["usd_rate"] = first.USDPrice.String()
		}
	}
	return data
}

func (p *TxBuilder) buildTransferOutData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["unique_id"] = tx.ID

	// Collect ALL outgoing transfers — symmetric to buildTransferInData.
	transfers := make([]map[string]interface{}, 0, len(tx.Transfers))
	var first *DecodedTransfer
	for i := range tx.Transfers {
		t := &tx.Transfers[i]
		if t.Direction != DirectionOut {
			continue
		}
		transfers = append(transfers, p.buildTransferEntry(t))
		if t.USDPrice == nil {
			// Outgoing unpriced transfer: enqueue a backfill job so the
			// pending disposal's proceeds_per_unit gets filled in later.
			p.ensureBackfillJob(ctx, t, tx.ChainID, tx.MinedAt)
		}
		if first == nil {
			first = t
		}
	}
	if first == nil && len(tx.Transfers) > 0 {
		first = &tx.Transfers[0]
		transfers = append(transfers, p.buildTransferEntry(first))
		if first.USDPrice == nil {
			p.ensureBackfillJob(ctx, first, tx.ChainID, tx.MinedAt)
		}
	}

	data["transfers"] = transfers

	// Legacy flat fields for back-compat.
	if first != nil {
		data["asset_id"] = first.AssetSymbol
		data["amount"] = money.NewBigInt(first.Amount).String()
		data["decimals"] = first.Decimals
		data["contract_address"] = legacyContractAddress(first.ContractAddress)
		data["to_address"] = first.Recipient
		if first.USDPrice != nil {
			data["usd_rate"] = first.USDPrice.String()
		}
	}
	// Map fee fields to gas fields expected by TransferOutHandler
	if feeAmt, ok := data["fee_amount"]; ok {
		data["gas_amount"] = feeAmt
	}
	if feeRate, ok := data["fee_usd_price"]; ok {
		data["gas_usd_rate"] = feeRate
	}
	if feeAsset, ok := data["fee_asset"]; ok {
		data["native_asset_id"] = feeAsset
	}
	return data
}

// buildTransferEntry renders a single DecodedTransfer into the map shape
// consumed by transfer/lending handlers (the "transfers" array element).
// Emits amount as a decimal string (money.BigInt-compatible) and omits
// usd_rate when unknown so handlers treat missing price as nil.
func (p *TxBuilder) buildTransferEntry(t *DecodedTransfer) map[string]interface{} {
	m := map[string]interface{}{
		"asset_id":         t.AssetSymbol,
		"amount":           money.NewBigInt(t.Amount).String(),
		"decimals":         t.Decimals,
		"contract_address": legacyContractAddress(t.ContractAddress),
		"direction":        string(t.Direction),
		"from_address":     t.Sender,
		"to_address":       t.Recipient,
	}
	if t.USDPrice != nil {
		m["usd_rate"] = t.USDPrice.String()
	}
	return m
}

// ensureBackfillJob upserts an asset by on-chain identity (if we have a
// contract address) and enqueues a price-backfill job keyed on
// (asset_id, target_time). Repo's Enqueue is idempotent on that pair, so
// it's safe to call for every unpriced transfer direction.
//
// This is the LEGACY identity path, writing to the `assets` table. It stays
// keyed on a real contract address and still skips the native coin — but the
// skip is now stated in terms of the sentinel rather than an empty string,
// because the adapter no longer spells native as "". Passing the sentinel
// through would reach normalizeContractAddress, fail the EVM shape check and
// produce an ErrInvalidContractAddress WARN on every native leg.
//
// Native coins are not missing an identity as a result: the (chain, contract)
// registry resolves them in resolveAssetIdentities, which runs for every leg of
// every transaction. What they lack here is a row in the legacy table, whose
// partial uniqueness index cannot represent them anyway — retiring that table
// is the contract phase (#59).
//
// Upsert failures are logged at WARN (sanitized fields) rather than
// swallowed silently, so the silent-dataloss pattern caught in Bug C is
// observable in logs and metrics.
func (p *TxBuilder) ensureBackfillJob(ctx context.Context, t *DecodedTransfer, chainID string, occurredAt time.Time) {
	if t == nil || chainID == "" {
		return
	}
	if t.ContractAddress == "" || t.ContractAddress == NativeContract {
		return
	}
	if p.assetUpsert == nil || p.jobEnqueuer == nil {
		return
	}
	name := t.AssetName
	if name == "" {
		name = t.AssetSymbol
	}
	a, _, err := p.assetUpsert.UpsertByOnChainIdentity(ctx, chainID, t.ContractAddress, t.AssetSymbol, name, t.Decimals)
	switch {
	case err == nil && a != nil:
		_, _ = p.jobEnqueuer.Enqueue(ctx, a.ID, occurredAt)
	case errors.Is(err, asset.ErrInvalidContractAddress):
		p.logger.Warn("invalid contract address from provider, asset cannot be priced",
			"chain_id", price.SanitizeLogField(chainID),
			"contract_address", price.SanitizeLogField(t.ContractAddress),
			"asset_symbol", price.SanitizeLogField(t.AssetSymbol),
		)
	case err != nil:
		p.logger.Warn("asset upsert failed during sync",
			"chain_id", price.SanitizeLogField(chainID),
			"contract_address", price.SanitizeLogField(t.ContractAddress),
			"error", price.SanitizeLogField(err.Error()),
		)
	}
}

func (p *TxBuilder) buildSwapData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)

	var transfersIn, transfersOut []map[string]interface{}
	for _, t := range tx.Transfers {
		td := p.buildSingleTransfer(ctx, t, tx.ChainID, tx.MinedAt)
		if t.Direction == DirectionIn {
			transfersIn = append(transfersIn, td)
		} else {
			transfersOut = append(transfersOut, td)
		}
	}
	data["transfers_in"] = transfersIn
	data["transfers_out"] = transfersOut
	return data
}

func (p *TxBuilder) buildInternalTransferData(w *wallet.Wallet, tx DecodedTransaction, destWalletID *uuid.UUID) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["source_wallet_id"] = w.ID.String()
	if destWalletID != nil {
		data["dest_wallet_id"] = destWalletID.String()
	}
	// A stitched bridge spans two chains (ADR-0002): the outflow is booked on
	// the chain the transaction was observed on and the inflow on DestChainID,
	// so the tax lot carries across instead of being disposed and re-acquired.
	// DestChainID is empty for everything a provider decodes, which leaves the
	// handler's same-chain default in place.
	if tx.DestChainID != "" && tx.DestChainID != tx.ChainID {
		data["source_chain_id"] = tx.ChainID
		data["dest_chain_id"] = tx.DestChainID
	}
	// InternalTransferHandler expects flat fields.
	// Extract the primary "out" transfer (from source wallet).
	var t *DecodedTransfer
	for i := range tx.Transfers {
		if tx.Transfers[i].Direction == DirectionOut {
			t = &tx.Transfers[i]
			break
		}
	}
	if t == nil && len(tx.Transfers) > 0 {
		t = &tx.Transfers[0]
	}
	if t != nil {
		data["asset_id"] = t.AssetSymbol
		data["amount"] = money.NewBigInt(t.Amount).String()
		data["decimals"] = t.Decimals
		data["contract_address"] = legacyContractAddress(t.ContractAddress)
		data["unique_id"] = tx.ID
		if t.USDPrice != nil {
			data["usd_rate"] = t.USDPrice.String()
		}
	}
	// Map fee fields to gas fields expected by InternalTransferHandler
	if feeAmt, ok := data["fee_amount"]; ok {
		data["gas_amount"] = feeAmt
	}
	if feeRate, ok := data["fee_usd_price"]; ok {
		data["gas_usd_rate"] = feeRate
	}
	if feeDec, ok := data["fee_decimals"]; ok {
		data["gas_decimals"] = feeDec
	}
	if feeAsset, ok := data["fee_asset"]; ok {
		data["native_asset_id"] = feeAsset
	}
	return data
}

func (p *TxBuilder) buildDeFiDepositData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *TxBuilder) buildDeFiWithdrawData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *TxBuilder) buildDeFiClaimData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *TxBuilder) buildLPDepositData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	if tx.NFTTokenID != "" {
		data["nft_token_id"] = tx.NFTTokenID
	}
	return data
}

func (p *TxBuilder) buildLPWithdrawData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *TxBuilder) buildLPClaimFeesData(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := p.buildBaseData(w, tx)
	data["transfers"] = p.buildTransferArray(ctx, tx.Transfers, tx.ChainID, tx.MinedAt)
	data["operation_type"] = string(tx.OperationType)
	return data
}

func (p *TxBuilder) buildBaseData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	data := map[string]interface{}{
		"wallet_id":   w.ID.String(),
		"tx_hash":     tx.TxHash,
		"chain_id":    tx.ChainID,
		"occurred_at": tx.MinedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if tx.Protocol != "" {
		data["protocol"] = tx.Protocol
	}
	if tx.Fee != nil {
		data["fee_asset"] = tx.Fee.AssetSymbol
		data["fee_amount"] = money.NewBigInt(tx.Fee.Amount).String()
		data["fee_decimals"] = tx.Fee.Decimals
		if tx.Fee.USDPrice != nil {
			data["fee_usd_price"] = tx.Fee.USDPrice.String()
		} else {
			data["fee_usd_price"] = "0"
		}
	}
	return data
}

func (p *TxBuilder) buildTransferArray(ctx context.Context, transfers []DecodedTransfer, chainID string, occurredAt time.Time) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(transfers))
	for _, t := range transfers {
		result = append(result, p.buildSingleTransfer(ctx, t, chainID, occurredAt))
	}
	return result
}

func (p *TxBuilder) buildSingleTransfer(ctx context.Context, t DecodedTransfer, chainID string, occurredAt time.Time) map[string]interface{} {
	m := map[string]interface{}{
		"asset_symbol":     t.AssetSymbol,
		"amount":           money.NewBigInt(t.Amount).String(),
		"decimals":         t.Decimals,
		"contract_address": legacyContractAddress(t.ContractAddress),
		"direction":        string(t.Direction),
		"sender":           t.Sender,
		"recipient":        t.Recipient,
	}

	if t.USDPrice != nil {
		// Price provided by the provider — use it directly.
		m["usd_price"] = t.USDPrice.String()
	} else if t.ContractAddress != "" && t.ContractAddress != NativeContract && chainID != "" {
		// The provider has no price for this token. Upsert the asset by on-chain
		// identity and enqueue a backfill job so the price is resolved later.
		// The sentinel is excluded for the same reason as in ensureBackfillJob:
		// this legacy path is keyed on a real contract address, and the native
		// coin's identity now comes from the registry instead.
		// Downstream readers treat the absence of the usd_price key as a
		// pending price (USDRate = nil, TaxLotHook creates a pending lot).
		// We do not emit any separate marker — the worker will fill it in.
		if p.assetUpsert != nil {
			name := t.AssetName
			if name == "" {
				name = t.AssetSymbol
			}
			a, _, err := p.assetUpsert.UpsertByOnChainIdentity(ctx, chainID, t.ContractAddress, t.AssetSymbol, name, t.Decimals)
			switch {
			case err == nil && a != nil:
				if p.jobEnqueuer != nil {
					_, _ = p.jobEnqueuer.Enqueue(ctx, a.ID, occurredAt)
				}
			case errors.Is(err, asset.ErrInvalidContractAddress):
				// The provider returned a contract address that failed our shape-check.
				// We cannot create an asset row for it, so there is nothing the
				// backfill worker could ever resolve. Proceed without a USD
				// rate; emit a WARN (sanitized) so the silent-dataloss pattern
				// is observable in logs / metrics.
				p.logger.Warn("invalid contract address from provider, asset cannot be priced",
					"chain_id", price.SanitizeLogField(chainID),
					"contract_address", price.SanitizeLogField(t.ContractAddress),
					"asset_symbol", price.SanitizeLogField(t.AssetSymbol),
				)
			default:
				// Any other error (DB-level, etc.). Log WARN, no enqueue.
				p.logger.Warn("asset upsert failed during sync",
					"chain_id", price.SanitizeLogField(chainID),
					"contract_address", price.SanitizeLogField(t.ContractAddress),
					"error", price.SanitizeLogField(err.Error()),
				)
			}
		}
	}
	// For native coins (ContractAddress == NativeContract) with no price: omit
	// usd_price entirely. Downstream handles the missing key as nil USDRate.

	return m
}

// --- Lending data builders ---

func (p *TxBuilder) buildLendingSupplyData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	// Supply: the principal being supplied flows OUT of the wallet. The aToken
	// the protocol mints back never arrives here — it is dropped at the
	// provider boundary as a receipt (#57), so what remains is the principal
	// and only the principal.
	return p.buildLendingData(w, tx, DirectionOut)
}

func (p *TxBuilder) buildLendingWithdrawData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	// Withdraw: the principal asset flows IN to the wallet, aToken flows OUT.
	return p.buildLendingData(w, tx, DirectionIn)
}

func (p *TxBuilder) buildLendingBorrowData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	// Borrow: both the debt receipt token AND the real borrowed asset
	// arrive as IN transfers. Prior to multi-asset support only the first
	// of these was persisted, so either the debt or the borrowed asset
	// was silently dropped.
	return p.buildLendingData(w, tx, DirectionIn)
}

func (p *TxBuilder) buildLendingRepayData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	// Repay: the repayment asset flows OUT; debt-token burn flows OUT too.
	return p.buildLendingData(w, tx, DirectionOut)
}

func (p *TxBuilder) buildLendingClaimData(w *wallet.Wallet, tx DecodedTransaction) map[string]interface{} {
	// Claim: rewards/interest flow IN; rarely a receipt-adjustment transfer.
	return p.buildLendingData(w, tx, DirectionIn)
}

// buildLendingData emits the shared lending data map shape. It collects all
// transfers matching the primary direction into `transfers`, then appends the
// opposite-direction ones, so the handler sees every leg of the op. It also
// picks the first primary transfer for the flat display fields (`asset`,
// `amount`, ...), which only the transactions reader consumes — the ledger
// entry builders read `transfers` exclusively.
func (p *TxBuilder) buildLendingData(w *wallet.Wallet, tx DecodedTransaction, primary TransferDirection) map[string]interface{} {
	data := p.buildBaseData(w, tx)

	transfers := make([]map[string]interface{}, 0, len(tx.Transfers))
	var first *DecodedTransfer
	for i := range tx.Transfers {
		t := &tx.Transfers[i]
		if t.Direction != primary {
			continue
		}
		transfers = append(transfers, p.buildTransferEntry(t))
		if first == nil {
			first = t
		}
	}
	// Include opposite-direction legs too. Since #57 these are never receipts —
	// those are gone before this point — but a real operation can still move
	// principal both ways (a supply that also returns dust, a repay that
	// reclaims excess). Handlers inspect direction to decide routing.
	for i := range tx.Transfers {
		t := &tx.Transfers[i]
		if t.Direction == primary {
			continue
		}
		transfers = append(transfers, p.buildTransferEntry(t))
	}

	data["transfers"] = transfers
	if first != nil {
		p.setLendingAssetFields(data, first)
	}
	return data
}

func (p *TxBuilder) findTransfer(transfers []DecodedTransfer, dir TransferDirection) *DecodedTransfer {
	for i := range transfers {
		if transfers[i].Direction == dir {
			return &transfers[i]
		}
	}
	if len(transfers) > 0 {
		return &transfers[0]
	}
	return nil
}

// setLendingAssetFields writes the flat display fields read by LendingReader
// (internal/module/transactions) for the transaction list and detail views.
// The ledger entry builders do not read them.
func (p *TxBuilder) setLendingAssetFields(data map[string]interface{}, t *DecodedTransfer) {
	data["asset"] = t.AssetSymbol
	data["amount"] = money.NewBigInt(t.Amount).String()
	data["decimals"] = t.Decimals
	data["contract_address"] = legacyContractAddress(t.ContractAddress)
	if t.USDPrice != nil {
		data["usd_price"] = t.USDPrice.String()
	}
}

// --- Lending position post-processing ---

func (p *TxBuilder) handleLendingSupply(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionOut)
	if t == nil {
		p.logger.Warn("lending supply: no outgoing transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending supply: failed to find or create position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordSupply(ctx, pos.ID, t.AssetSymbol, t.Decimals, legacyContractAddress(t.ContractAddress), t.Amount, usdValue); err != nil {
		p.logger.Error("lending supply: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *TxBuilder) handleLendingWithdraw(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionIn)
	if t == nil {
		p.logger.Warn("lending withdraw: no incoming transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending withdraw: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordWithdraw(ctx, pos.ID, t.AssetSymbol, t.Amount, usdValue); err != nil {
		p.logger.Error("lending withdraw: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *TxBuilder) handleLendingBorrow(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionIn)
	if t == nil {
		p.logger.Warn("lending borrow: no incoming transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending borrow: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordBorrow(ctx, pos.ID, t.AssetSymbol, t.Decimals, legacyContractAddress(t.ContractAddress), t.Amount, usdValue); err != nil {
		p.logger.Error("lending borrow: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *TxBuilder) handleLendingRepay(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionOut)
	if t == nil {
		p.logger.Warn("lending repay: no outgoing transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending repay: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordRepay(ctx, pos.ID, t.AssetSymbol, t.Amount, usdValue); err != nil {
		p.logger.Error("lending repay: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

func (p *TxBuilder) handleLendingClaim(ctx context.Context, w *wallet.Wallet, tx DecodedTransaction) {
	t := p.findTransfer(tx.Transfers, DirectionIn)
	if t == nil {
		p.logger.Warn("lending claim: no incoming transfer", "tx_hash", tx.TxHash)
		return
	}

	pos, err := p.lendingPositionSvc.FindOrCreate(ctx, w.UserID, w.ID,
		p.lendingProtocol(tx), tx.ChainID, tx.MinedAt,
	)
	if err != nil {
		p.logger.Error("lending claim: failed to find position", "tx_hash", tx.TxHash, "error", err)
		return
	}

	usdValue := p.calcLendingUSD(t)
	if err := p.lendingPositionSvc.RecordClaim(ctx, pos.ID, usdValue); err != nil {
		p.logger.Error("lending claim: failed to record", "tx_hash", tx.TxHash, "position_id", pos.ID, "error", err)
	}
}

// lendingProtocol returns the protocol name for lending operations,
// defaulting to "AAVE" when the provider does not tag the protocol.
func (p *TxBuilder) lendingProtocol(tx DecodedTransaction) string {
	if tx.Protocol != "" {
		return tx.Protocol
	}
	return "AAVE"
}

func (p *TxBuilder) calcLendingUSD(t *DecodedTransfer) *big.Int {
	if t.USDPrice != nil && t.Amount != nil {
		return money.CalcUSDValue(t.Amount, t.USDPrice, t.Decimals)
	}
	return big.NewInt(0)
}
