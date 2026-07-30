package sync

import (
	"strings"

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

	// Uniswap V3 LP-specific classification
	if c.isUniswapV3(tx.Protocol) {
		if lpType := c.classifyLP(tx); lpType != "" {
			return lpType
		}
	}

	// AAVE lending protocol (by protocol name or by asset heuristics)
	if c.isAAVE(tx.Protocol) || c.hasAaveAssets(tx.Transfers) {
		if lt := c.classifyLending(tx); lt != "" {
			return lt
		}
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

func (c *Classifier) isUniswapV3(protocol string) bool {
	return protocol == "Uniswap V3"
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

func (c *Classifier) isAAVE(protocol string) bool {
	return protocol == "AAVE" || protocol == "Aave" || protocol == "Aave V3" || protocol == "Aave V2"
}

// hasAaveAssets detects AAVE lending transactions by transfer asset names/symbols
// when the provider does not tag the protocol. Aave aTokens and debt tokens have
// distinctive naming: aEthWETH, variableDebtBasUSDC, stableDebtEthDAI, etc.
func (c *Classifier) hasAaveAssets(transfers []DecodedTransfer) bool {
	for _, t := range transfers {
		if strings.HasPrefix(t.AssetName, "Aave ") {
			return true
		}
		if strings.HasPrefix(t.AssetSymbol, "variableDebt") ||
			strings.HasPrefix(t.AssetSymbol, "stableDebt") {
			return true
		}
		// aToken pattern: lowercase 'a' + uppercase letter (e.g. aEthWETH, aBasUSDC)
		if len(t.AssetSymbol) > 2 && t.AssetSymbol[0] == 'a' &&
			t.AssetSymbol[1] >= 'A' && t.AssetSymbol[1] <= 'Z' {
			return true
		}
	}
	return false
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
