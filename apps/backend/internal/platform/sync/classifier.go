package sync

import "github.com/kislikjeka/moontrack/internal/ledger"

// Classifier maps decoded blockchain transactions to ledger transaction types
type Classifier struct{}

// NewClassifier creates a new Classifier
func NewClassifier() *Classifier {
	return &Classifier{}
}

// Classify determines the ledger TransactionType for a decoded transaction.
// Returns empty string for transactions that should be skipped (e.g. approve).
func (c *Classifier) Classify(tx DecodedTransaction) ledger.TransactionType {
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
