package sync_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

func TestClassifier_DirectMappings(t *testing.T) {
	c := sync.NewClassifier()

	tests := []struct {
		name     string
		opType   sync.OperationType
		expected ledger.TransactionType
	}{
		{"trade -> swap", sync.OpTrade, ledger.TxTypeSwap},
		{"deposit -> defi_deposit", sync.OpDeposit, ledger.TxTypeDefiDeposit},
		{"withdraw -> defi_withdraw", sync.OpWithdraw, ledger.TxTypeDefiWithdraw},
		{"claim -> defi_claim", sync.OpClaim, ledger.TxTypeDefiClaim},
		{"receive -> transfer_in", sync.OpReceive, ledger.TxTypeTransferIn},
		{"send -> transfer_out", sync.OpSend, ledger.TxTypeTransferOut},
		{"mint -> defi_deposit", sync.OpMint, ledger.TxTypeDefiDeposit},
		{"burn -> defi_withdraw", sync.OpBurn, ledger.TxTypeDefiWithdraw},
		{"approve -> skip", sync.OpApprove, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := sync.DecodedTransaction{
				OperationType: tt.opType,
				Transfers: []sync.DecodedTransfer{
					{Direction: sync.DirectionIn, Amount: big.NewInt(1)},
				},
			}
			result := c.Classify(tx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifier_Execute_InferFromTransfers(t *testing.T) {
	c := sync.NewClassifier()

	t.Run("in only -> transfer_in", func(t *testing.T) {
		tx := sync.DecodedTransaction{
			OperationType: sync.OpExecute,
			Transfers: []sync.DecodedTransfer{
				{Direction: sync.DirectionIn, Amount: big.NewInt(100)},
			},
		}
		assert.Equal(t, ledger.TxTypeTransferIn, c.Classify(tx))
	})

	t.Run("out only -> transfer_out", func(t *testing.T) {
		tx := sync.DecodedTransaction{
			OperationType: sync.OpExecute,
			Transfers: []sync.DecodedTransfer{
				{Direction: sync.DirectionOut, Amount: big.NewInt(100)},
			},
		}
		assert.Equal(t, ledger.TxTypeTransferOut, c.Classify(tx))
	})

	t.Run("in and out -> swap", func(t *testing.T) {
		tx := sync.DecodedTransaction{
			OperationType: sync.OpExecute,
			Transfers: []sync.DecodedTransfer{
				{Direction: sync.DirectionOut, Amount: big.NewInt(100)},
				{Direction: sync.DirectionIn, Amount: big.NewInt(200)},
			},
		}
		assert.Equal(t, ledger.TxTypeSwap, c.Classify(tx))
	})

	t.Run("no transfers -> skip", func(t *testing.T) {
		tx := sync.DecodedTransaction{
			OperationType: sync.OpExecute,
			Transfers:     []sync.DecodedTransfer{},
		}
		assert.Equal(t, ledger.TransactionType(""), c.Classify(tx))
	})
}

func TestClassifier_UnknownOpType_FallsBackToExecute(t *testing.T) {
	c := sync.NewClassifier()

	tx := sync.DecodedTransaction{
		OperationType: "unknown_op",
		Transfers: []sync.DecodedTransfer{
			{Direction: sync.DirectionIn, Amount: big.NewInt(100)},
			{Direction: sync.DirectionOut, Amount: big.NewInt(50)},
		},
	}
	assert.Equal(t, ledger.TxTypeSwap, c.Classify(tx))
}

func TestClassifier_Execute_MultipleTransfersSameDirection(t *testing.T) {
	c := sync.NewClassifier()

	tx := sync.DecodedTransaction{
		OperationType: sync.OpExecute,
		Transfers: []sync.DecodedTransfer{
			{Direction: sync.DirectionIn, Amount: big.NewInt(100)},
			{Direction: sync.DirectionIn, Amount: big.NewInt(200)},
		},
	}
	assert.Equal(t, ledger.TxTypeTransferIn, c.Classify(tx))
}

func TestClassify_UniV3Deposit(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpDeposit,
		Protocol:      "Uniswap V3",
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPDeposit, c.Classify(tx))
}

func TestClassify_UniV3Withdraw(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpWithdraw,
		Protocol:      "Uniswap V3",
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionIn, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPWithdraw, c.Classify(tx))
}

func TestClassify_UniV3Mint(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpMint,
		Protocol:      "Uniswap V3",
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPDeposit, c.Classify(tx))
}

func TestClassify_UniV3Burn(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpBurn,
		Protocol:      "Uniswap V3",
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionIn, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPWithdraw, c.Classify(tx))
}

func TestClassify_UniV3ClaimFees(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpReceive,
		Protocol:      "Uniswap V3",
		Acts:          []string{"claim"},
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionIn, AssetSymbol: "USDC", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPClaimFees, c.Classify(tx))
}

func TestClassify_NonUniDeposit_StaysDeFi(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpDeposit,
		Protocol:      "Aave",
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeDefiDeposit, c.Classify(tx))
}

func TestClassify_ReceiveNonClaim_StaysTransferIn(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpReceive,
		Protocol:      "Uniswap V3",
		Acts:          []string{"execute"},
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionIn, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeTransferIn, c.Classify(tx))
}
