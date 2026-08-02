package sync

import (
	"github.com/kislikjeka/moontrack/internal/ledger"
)

// Classifier maps decoded blockchain transactions to ledger transaction types
type Classifier struct{}

// NewClassifier creates a new Classifier
func NewClassifier() *Classifier {
	return &Classifier{}
}

// Classify determines the ledger TransactionType for a decoded transaction.
// Returns empty string for transactions that should be skipped (e.g. approve, NFT-only).
func (c *Classifier) Classify(tx DecodedTransaction) ledger.TransactionType {
	if len(tx.Transfers) == 0 && tx.OperationType != OpApprove {
		return "" // no fungible transfers to process (e.g., NFT-only transaction)
	}

	// Liquidity-pool and lending-market classification, decided by the actions
	// the provider stamped on the transaction's legs (issue #57).
	//
	// This used to be decided by protocol NAME, matched against two string
	// literals ("Uniswap V3", "AAVE") that the adapter had itself manufactured
	// by scanning party names, backed by a fallback that sniffed Aave's ticker
	// conventions off the transfers. Both were reading a vendor's branding to
	// infer an operation. The provider names the operation directly, per leg —
	// `liquidityAdded`, `collateralSharesMinted`, `borrowed` — so the operation
	// is read instead of inferred, and a protocol the hardcoded pair never
	// heard of classifies the same as the two that were named.
	if anyActionIn(tx.LegActions, liquidityActions) {
		if lpType := c.classifyLP(tx); lpType != "" {
			return lpType
		}
	}

	if anyActionIn(tx.LegActions, lendingActions) {
		if lt := c.classifyLending(tx); lt != "" {
			return lt
		}
	}

	// A reward claim, named as such by the provider and by nothing else.
	//
	// `rewardsReceived` is PRINCIPAL — a reward is a genuine acquisition and
	// stays a position, unlike the receipt tokens this pass drops — but it is
	// not an ordinary inbound transfer either, and the operation type cannot
	// say so: the adapter maps claimRewards onto OpReceive, which alone reads
	// as transfer_in.
	//
	// It books as the protocol-NEUTRAL defi_claim rather than lp_claim_fees or
	// lending_claim, because on real data the transaction carries no other
	// protocol action to say which market it came from — a claim arrives as
	// rewards in and gas out, nothing more. The old code answered by matching a
	// party name against the literal "Uniswap V3" and calling every hit an LP
	// fee collection; that was a guess dressed as a fact, and wrong for every
	// protocol outside the two it knew. Claiming less is the accurate answer.
	if anyActionIn(tx.LegActions, rewardActions) {
		return ledger.TxTypeDefiClaim
	}

	// A ROUND-TRIP bridge leg (issue #33, ADR-0002): the provider calls it a
	// bridge, but a DIFFERENT asset came straight back to the same wallet within
	// the one transaction. That is a bridge-as-swap, not a cross-chain leg, and
	// it must be booked locally as the swap it is.
	//
	// This has to precede the OperationType switch. The adapter maps
	// sendToBridge onto OpSend and receiveFromBridge onto OpReceive, and those
	// cases return transfer_out / transfer_in outright — which for a round-trip
	// would record only one side of the trade and silently drop the asset
	// received in exchange, losing both the acquisition and its cost basis.
	//
	// The test is a differing asset, not merely both directions, and that
	// distinction is load-bearing on real data: most genuine pure-sends carry a
	// same-asset dust refund, and getting a sliver of your own asset back is not
	// a trade — booking it as a swap would fabricate a disposal of an asset
	// against itself. Ordinary sends and receives are untouched entirely: the
	// rule is scoped to legs the provider itself identified as a bridge.
	// isCrossAssetRoundTrip is shared with the stitcher, which defines a
	// stitchable pure-send as its exact complement. The two must agree: if they
	// diverged, a leg could be booked as a swap here while the stitcher still
	// carried the same value across a bridge.
	if c.isBridgeLeg(tx) && isCrossAssetRoundTrip(tx.Transfers) {
		return ledger.TxTypeSwap
	}

	switch tx.OperationType {
	case OpTrade:
		return ledger.TxTypeSwap
	case OpDeposit:
		return ledger.TxTypeDefiDeposit
	case OpWithdraw:
		return ledger.TxTypeDefiWithdraw
	case OpClaim:
		return ledger.TxTypeDefiClaim
	case OpReceive:
		return ledger.TxTypeTransferIn
	case OpSend:
		return ledger.TxTypeTransferOut
	case OpMint:
		return ledger.TxTypeDefiDeposit
	case OpBurn:
		return ledger.TxTypeDefiWithdraw
	case OpExecute:
		return c.classifyExecute(tx)
	case OpApprove:
		return "" // skip approvals
	default:
		return c.classifyExecute(tx) // fallback: infer from transfers
	}
}

// anyActionIn reports whether any leg of the transaction did something in the
// given set of actions.
func anyActionIn(legActions []string, set map[string]bool) bool {
	for _, a := range legActions {
		if set[a] {
			return true
		}
	}
	return false
}

// isBridgeLeg reports whether the provider classified this transaction as one
// side of a cross-chain bridge. ProviderType is the only place that survives:
// the adapter collapses sendToBridge onto OpSend and receiveFromBridge onto
// OpReceive, making a bridge leg indistinguishable from an ordinary transfer by
// operation type alone.
func (c *Classifier) isBridgeLeg(tx DecodedTransaction) bool {
	return tx.ProviderType == providerTypeSendToBridge ||
		tx.ProviderType == providerTypeReceiveFromBridge
}

func (c *Classifier) classifyLP(tx DecodedTransaction) ledger.TransactionType {
	switch tx.OperationType {
	case OpDeposit, OpMint:
		return ledger.TxTypeLPDeposit
	case OpWithdraw, OpBurn:
		return ledger.TxTypeLPWithdraw
	case OpReceive:
		if c.hasClaimAct(tx.Acts) {
			return ledger.TxTypeLPClaimFees
		}
		return "" // fall through to default classification
	default:
		return "" // fall through to default classification
	}
}

func (c *Classifier) hasClaimAct(acts []string) bool {
	for _, act := range acts {
		if act == "claim" {
			return true
		}
	}
	return false
}

// classifyExecute infers transaction type from transfer directions when
// the operation type is "execute" or unknown.
func (c *Classifier) classifyExecute(tx DecodedTransaction) ledger.TransactionType {
	if len(tx.Transfers) == 0 {
		return "" // no transfers to classify, skip
	}

	hasIn, hasOut := directions(tx.Transfers)

	switch {
	case hasIn && hasOut:
		return ledger.TxTypeSwap // both directions = swap
	case hasIn:
		return ledger.TxTypeTransferIn
	case hasOut:
		return ledger.TxTypeTransferOut
	default:
		return "" // skip
	}
}

// directions reports whether transfers carry value into and out of the wallet.
func directions(transfers []DecodedTransfer) (hasIn, hasOut bool) {
	for _, t := range transfers {
		switch t.Direction {
		case DirectionIn:
			hasIn = true
		case DirectionOut:
			hasOut = true
		}
	}
	return hasIn, hasOut
}

func (c *Classifier) classifyLending(tx DecodedTransaction) ledger.TransactionType {
	switch tx.OperationType {
	case OpDeposit, OpMint:
		return ledger.TxTypeLendingSupply
	case OpWithdraw, OpBurn:
		return ledger.TxTypeLendingWithdraw
	case OpClaim:
		return ledger.TxTypeLendingClaim
	case OpReceive:
		if c.hasClaimAct(tx.Acts) {
			return ledger.TxTypeLendingClaim
		}
		return ledger.TxTypeLendingBorrow
	case OpSend:
		return ledger.TxTypeLendingRepay
	default:
		return c.classifyLendingFromTransfers(tx)
	}
}

func (c *Classifier) classifyLendingFromTransfers(tx DecodedTransaction) ledger.TransactionType {
	hasIn, hasOut := directions(tx.Transfers)
	if hasIn && !hasOut {
		return ledger.TxTypeLendingBorrow
	}
	if hasOut && !hasIn {
		return ledger.TxTypeLendingRepay
	}
	if hasIn && hasOut {
		return ledger.TxTypeLendingSupply
	}
	return ""
}
