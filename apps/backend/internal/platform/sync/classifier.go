package sync

import "github.com/kislikjeka/moontrack/internal/ledger"

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

	// AAVE lending protocol
	if c.isAAVE(tx.Protocol) {
		if lt := c.classifyLending(tx); lt != "" {
			return lt
		}
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

	hasIn := false
	hasOut := false
	for _, t := range tx.Transfers {
		if t.Direction == DirectionIn {
			hasIn = true
		}
		if t.Direction == DirectionOut {
			hasOut = true
		}
	}

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

func (c *Classifier) isAAVE(protocol string) bool {
	return protocol == "AAVE" || protocol == "Aave" || protocol == "Aave V3" || protocol == "Aave V2"
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
	hasIn := false
	hasOut := false
	for _, t := range tx.Transfers {
		if t.Direction == DirectionIn {
			hasIn = true
		}
		if t.Direction == DirectionOut {
			hasOut = true
		}
	}
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
