package noves

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/pkg/money"
)

// SyncAdapter adapts the Noves client to the sync.TransactionDataProvider port.
// Scope of issue #25: transactions only. Positions/balances (the reconciler)
// are a separate downstream ticket, so this adapter does NOT implement
// sync.PositionDataProvider.
type SyncAdapter struct {
	client *Client
}

// Compile-time check that SyncAdapter implements TransactionDataProvider.
var _ sync.TransactionDataProvider = (*SyncAdapter)(nil)

// NewSyncAdapter creates a new Noves sync adapter.
func NewSyncAdapter(client *Client) *SyncAdapter {
	return &SyncAdapter{client: client}
}

// GetTransactions fetches decoded transactions for a single chain and converts
// them to domain types. The port is chain-aware: the caller passes a domain
// chain slug (Zerion-style, e.g. "ethereum", "base"); we map it to the Noves
// short slug for the endpoint and emit the domain slug back on each result.
func (a *SyncAdapter) GetTransactions(ctx context.Context, address, chain string, since time.Time) ([]sync.DecodedTransaction, error) {
	var all []sync.DecodedTransaction
	err := a.StreamTransactions(ctx, address, chain, since, func(page []sync.DecodedTransaction) error {
		all = append(all, page...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// StreamTransactions fetches decoded transactions for a single chain page by
// page, oldest page first, converting each page to domain types before handing
// it to onPage. This lets the collector persist incrementally so an interrupted
// deep sync resumes forward rather than restarting (issue #29).
func (a *SyncAdapter) StreamTransactions(
	ctx context.Context,
	address, chain string,
	since time.Time,
	onPage func([]sync.DecodedTransaction) error,
) error {
	novesChain, ok := domainToNovesChain(chain)
	if !ok {
		return nil // unsupported chain: nothing to fetch
	}

	return a.client.StreamTransactions(ctx, novesChain, address, since, func(txs []Transaction) error {
		page := make([]sync.DecodedTransaction, 0, len(txs))
		for _, tx := range txs {
			dt, err := convertTransaction(tx, chain)
			if err != nil {
				continue // skip individual conversion failures
			}
			page = append(page, dt)
		}
		if len(page) == 0 {
			return nil
		}
		return onPage(page)
	})
}

// convertTransaction maps a Noves Transaction to a domain DecodedTransaction.
// domainChain is the canonical (Zerion-style) chain slug emitted on the result.
func convertTransaction(tx Transaction, domainChain string) (sync.DecodedTransaction, error) {
	txHash := tx.RawTransactionData.TransactionHash
	if txHash == "" {
		return sync.DecodedTransaction{}, fmt.Errorf("missing transaction hash")
	}

	var (
		transfers    []sync.DecodedTransfer
		rejected     []sync.RejectedLeg
		legActions   []string
		nftTokenID   string
		needsReview  bool
		reviewReason string
	)

	// sent[] → out, received[] → in. paidGas legs are filtered (double-counts the
	// fee). NFT legs are not emitted as fungible transfers; their id is captured.
	appendLeg := func(t Transfer, dir sync.TransferDirection) {
		if t.Action == "paidGas" {
			return
		}

		// Every leg's action is recorded before any leg is dropped, receipts
		// included. The action is what identifies the SHAPE of the transaction
		// — a `collateralSharesMinted` says "lending market" no matter what the
		// protocol field says — and on a lending supply the receipt leg is one
		// of only two legs there are. Collecting after the drop would throw away
		// the evidence that the drop was correct.
		if t.Action != "" {
			legActions = append(legActions, t.Action)
		}

		if t.NFT != nil && t.Token == nil {
			if nftTokenID == "" && t.NFT.ID.String() != "" {
				nftTokenID = t.NFT.ID.String()
			}
			return // NFT-only leg: not a fungible transfer
		}

		// A PROTOCOL RECEIPT never becomes a transfer (issue #57). The aToken
		// minted against a supply, the debt token minted against a borrow, the
		// LP token minted against added liquidity: each records a position the
		// protocol already holds for the user, and booking it beside the
		// principal it was minted against records one movement twice.
		//
		// This is decided from the leg's action, and deliberately not from the
		// token: the receipt token is a genuine, quoted asset, so no property of
		// the token can answer the question. It is decided HERE, at the provider
		// boundary, because the action is provider vocabulary — everything
		// downstream sees a transaction whose legs are all principal.
		//
		// The check runs after the NFT branch on purpose. `lpTokensMinted`
		// arrives as an NFT-only leg on Uniswap V3, carrying no symbol and no
		// contract but carrying the position id that LP tracking is keyed on;
		// dropping it earlier would discard the id along with the receipt.
		//
		// The leg is RECORDED as rejected rather than vanishing (issue #60). The
		// receipt token is real and quoted, so the provider reports a POSITION in
		// it; with the leg gone without trace, reconciliation compared that
		// position against a net flow of zero and flagged the chain for a balance
		// the rule had deliberately and correctly excluded. Keeping the identity
		// here — the one place it still exists, since the raw is written from what
		// this function returns — is what lets the delta and the report explain
		// that position the same way.
		if sync.IsReceiptLeg(t.Action) {
			rejected = append(rejected, rejectedLeg(t, domainChain, dir, sync.RejectionReceipt))
			return
		}

		if t.Token == nil {
			return // neither token nor NFT: nothing to record
		}
		dt, review := convertTransfer(t, dir)
		transfers = append(transfers, dt)
		if review != "" {
			needsReview = true
			if reviewReason == "" {
				reviewReason = review
			}
		}
	}

	for _, t := range tx.ClassificationData.Sent {
		appendLeg(t, sync.DirectionOut)
	}
	for _, t := range tx.ClassificationData.Received {
		appendLeg(t, sync.DirectionIn)
	}

	fee := convertFee(tx.RawTransactionData.TransactionFee)

	return sync.DecodedTransaction{
		ID:            externalID(domainChain, txHash),
		TxHash:        txHash,
		ChainID:       domainChain,
		OperationType: mapOperationType(tx.ClassificationData.Type),
		Protocol:      protocolName(tx),
		Transfers:     transfers,
		RejectedLegs:  rejected,
		LegActions:    legActions,
		Fee:           fee,
		MinedAt:       time.Unix(tx.RawTransactionData.Timestamp, 0).UTC(),
		Status:        statusOf(tx.ClassificationData.Type),
		NFTTokenID:    nftTokenID,
		Acts:          collectActs(tx),
		NeedsReview:   needsReview,
		ReviewReason:  reviewReason,
		Unclassified:  isUnclassifiedType(tx.ClassificationData.Type),
		ProviderType:  tx.ClassificationData.Type,
	}, nil
}

// convertTransfer maps a Noves Transfer to a domain DecodedTransfer. It returns
// a non-empty review reason when the amount could not be converted exactly
// (more fractional digits than the token's decimals → base-unit truncation).
func convertTransfer(t Transfer, dir sync.TransferDirection) (sync.DecodedTransfer, string) {
	decimals := 0
	symbol := ""
	name := ""
	contract := ""
	if t.Token != nil {
		decimals = t.Token.Decimals
		symbol = t.Token.Symbol
		name = t.Token.Name
		contract = normalizeContract(t.Token.Address)
	}

	amount, review := amountToBaseUnits(t.Amount, decimals, symbol)

	return sync.DecodedTransfer{
		AssetSymbol:     symbol,
		AssetName:       name,
		ContractAddress: contract,
		Decimals:        decimals,
		Amount:          amount,
		Direction:       dir,
		Sender:          partyAddress(t.From),
		Recipient:       partyAddress(t.To),
	}, review
}

// rejectedLeg records a leg that a rule kept out of the ledger, preserving the
// asset identity the rule acted on (issue #60).
//
// It deliberately does NOT reuse convertTransfer. A rejected leg is not a
// transfer: it must never be mistaken for one by anything that later reads a
// []DecodedTransfer, and the two carry different fields — a rejection needs the
// reason and the action, and has no use for sender, recipient or price.
//
// chain is passed in rather than taken from the transaction because a leg's
// chain is not always the observed one; the caller knows which applies.
func rejectedLeg(t Transfer, chain string, dir sync.TransferDirection, reason sync.RejectionReason) sync.RejectedLeg {
	decimals := 0
	symbol := ""
	contract := ""
	if t.Token != nil {
		decimals = t.Token.Decimals
		symbol = t.Token.Symbol
		contract = normalizeContract(t.Token.Address)
	}

	// The truncation flag is dropped on purpose: a rejected leg is never booked,
	// so an inexact amount cannot corrupt a balance. It is reported for size, and
	// a truncated size is still the right order of magnitude.
	amount, _ := amountToBaseUnits(t.Amount, decimals, symbol)

	return sync.RejectedLeg{
		ChainID:         chain,
		ContractAddress: contract,
		AssetSymbol:     symbol,
		Decimals:        decimals,
		Amount:          amount,
		Direction:       dir,
		Reason:          reason,
		Action:          t.Action,
	}
}

// convertFee maps the raw transaction fee to a domain DecodedFee. Returns nil
// when no fee token is present.
func convertFee(fee Fee) *sync.DecodedFee {
	if fee.Token == nil || fee.Amount.String() == "" {
		return nil
	}
	amount, _ := amountToBaseUnits(fee.Amount.String(), fee.Token.Decimals, fee.Token.Symbol)
	return &sync.DecodedFee{
		AssetSymbol: fee.Token.Symbol,
		AssetName:   fee.Token.Name,
		Amount:      amount,
		Decimals:    fee.Token.Decimals,
	}
}

// amountToBaseUnits converts a human-unit decimal string to base units. When the
// decimal has more fractional digits than decimals, money.ToBaseUnits would
// silently truncate — we detect that, still return the truncated value (so the
// transfer is not lost), and return a review reason so the transaction is
// flagged rather than silently floored.
func amountToBaseUnits(amountStr string, decimals int, symbol string) (*big.Int, string) {
	amount, err := money.ToBaseUnits(amountStr, decimals)
	if err != nil || amount == nil {
		return big.NewInt(0), ""
	}

	if fractionalDigits(amountStr) > decimals {
		reason := fmt.Sprintf("amount %q has more fractional digits than %s decimals (%d): base-unit conversion truncates", amountStr, symbol, decimals)
		return amount, reason
	}
	return amount, ""
}

// fractionalDigits returns the number of digits after the decimal point.
func fractionalDigits(amountStr string) int {
	i := strings.IndexByte(amountStr, '.')
	if i < 0 {
		return 0
	}
	return len(amountStr) - i - 1
}

// normalizeContract lowercases a hex contract address, or returns the native
// sentinel sync.NativeContract for the native coin. Noves encodes native as a
// symbol-as-address sentinel (no 0x prefix, address == symbol, e.g. "ETH"), and
// this is the one place that provider spelling is translated into MoonTrack's.
//
// The sentinel is a literal rather than the empty string it used to be (issue
// #56): an empty contract is indistinguishable from "the provider told us
// nothing", so downstream code had to guess, and every site guessed by skipping
// the leg. A named value makes the native case something the pipeline carries
// explicitly instead of something it silently drops.
//
// Address casing is normalized to lowercase for tokens. EVM addresses are
// case-insensitive and providers are inconsistent about checksum casing, so
// without this the same contract yields two identities.
func normalizeContract(address string) string {
	if isNativeAddress(address) {
		return sync.NativeContract
	}
	return strings.ToLower(address)
}

// isNativeAddress reports whether a token address is the native-coin sentinel:
// empty, or a non-hex marker (equal to the symbol on real data, e.g. "ETH").
func isNativeAddress(address string) bool {
	if address == "" {
		return true
	}
	return !strings.HasPrefix(strings.ToLower(address), "0x")
}

// partyAddress returns the lowercased address of a party, or "" if absent.
func partyAddress(p Party) string {
	if p.Address == nil {
		return ""
	}
	return strings.ToLower(*p.Address)
}

// externalID builds the canonical external id: "chain:txHash", lowercased.
func externalID(chain, txHash string) string {
	return strings.ToLower(chain + ":" + txHash)
}

// statusOf maps a Noves classification type to a domain status.
func statusOf(novesType string) string {
	if novesType == "failed" {
		return "failed"
	}
	return "confirmed"
}

// novesTypeToOperation maps Noves' rich classification types onto the existing
// OperationType vocabulary the classifier switches on (Zerion-derived). This
// keeps classifier.go untouched — its contract is the OperationType enum, not
// the raw provider strings.
//
// Membership in this table is also what "the provider classified it" means:
// isUnclassifiedType reads the same table, so a type can never be routed here
// and simultaneously counted as unknown.
//
// Claim types map to OpReceive, not OpClaim: the classifier routes claims
// through the OpReceive + hasClaimAct path (LP fee claims and lending reward
// claims both arrive as inbound transfers carrying a "claim" act — see
// classifyLP/classifyLending). There is no standalone OpClaim case in
// classifyLP, so OpReceive is what makes LPClaimFees/LendingClaim fire.
var novesTypeToOperation = map[string]sync.OperationType{
	"swap": sync.OpTrade,

	"depositCollateral": sync.OpDeposit,
	"addLiquidity":      sync.OpDeposit,
	"deposit":           sync.OpDeposit,
	"stakeToken":        sync.OpDeposit,
	"lend":              sync.OpDeposit,

	"removeLiquidity":    sync.OpWithdraw,
	"withdrawCollateral": sync.OpWithdraw,
	"withdraw":           sync.OpWithdraw,
	"unstakeToken":       sync.OpWithdraw,
	"borrow":             sync.OpWithdraw,

	"claimRewards": sync.OpReceive,
	"claim":        sync.OpReceive,

	"receiveToken":        sync.OpReceive,
	"receiveTokenAirdrop": sync.OpReceive,
	"receiveSpamToken":    sync.OpReceive,
	"receiveFromBridge":   sync.OpReceive,
	"received":            sync.OpReceive,

	"sendToken":    sync.OpSend,
	"sendToBridge": sync.OpSend,
	"sent":         sync.OpSend,

	"approveToken": sync.OpApprove,
	"revokeToken":  sync.OpApprove,

	// A failed transaction is classified — what it *would* have done is
	// irrelevant, and statusOf marks it failed so the TxBuilder skips it before
	// classification. Mapped explicitly so it never counts as unknown.
	"failed": sync.OpExecute,
}

// mapOperationType maps a Noves classification type to a domain OperationType.
// Unmapped types (including `unclassified`) fall back to execute — the
// classifier then infers from transfer directions.
func mapOperationType(novesType string) sync.OperationType {
	if op, ok := novesTypeToOperation[novesType]; ok {
		return op
	}
	return sync.OpExecute
}

// isUnclassifiedType reports whether Noves could not identify what the
// transaction did: either it said so outright (`unclassified`,
// `unverifiedContract`) or it returned a type this adapter has no mapping for,
// which to MoonTrack is equally unknown. The distinction is load-bearing
// because mapOperationType collapses several classified types onto OpExecute
// alongside the unknown ones (issue #30).
func isUnclassifiedType(novesType string) bool {
	_, known := novesTypeToOperation[novesType]
	return !known
}

// collectActs builds the Acts slice the classifier reads (hasClaimAct → needs
// "claim"). Noves uses claimRewards/rewardsReceived rather than a bare "claim",
// so we add "claim" when the classification is claim-ish, plus the raw type and
// distinct per-transfer actions (minus paidGas) for downstream visibility.
func collectActs(tx Transaction) []string {
	seen := map[string]bool{}
	var acts []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		acts = append(acts, s)
	}

	t := tx.ClassificationData.Type
	add(t)
	if t == "claimRewards" || t == "claim" {
		add("claim")
	}

	for _, leg := range tx.ClassificationData.Sent {
		if leg.Action != "paidGas" {
			add(leg.Action)
		}
	}
	for _, leg := range tx.ClassificationData.Received {
		if leg.Action != "paidGas" {
			add(leg.Action)
		}
	}
	return acts
}

// protocolName returns the protocol the provider named, or "" when it named
// none. It is a label carried for display and for the lending-position
// aggregate; nothing classifies on it.
//
// It used to guess. `protocol.name` is null on most real data, so the old
// deriveProtocol scanned party names and NFT names for two hardcoded markers —
// the literal "Uniswap V3" and the prefix "Aave" — and manufactured a protocol
// string from them, which the classifier then matched against the same two
// literals to decide LP versus lending. That recognized exactly two protocols
// by name and silently degraded every other one to a generic deposit, and it
// could not express what it was actually looking for: the QuickSwap fixture's
// UNI-V2 receipt matches no marker at all. The classifier now reads the
// provider's per-leg actions instead (issue #57), which name the operation
// rather than the vendor, so the guess has nothing left to feed.
func protocolName(tx Transaction) string {
	if tx.ClassificationData.Protocol.Name != nil {
		return *tx.ClassificationData.Protocol.Name
	}
	return ""
}
