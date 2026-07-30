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

func TestClassify_AaveDeposit_IsLendingSupply(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpDeposit,
		Protocol:      "Aave",
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLendingSupply, c.Classify(tx))
}

func TestClassify_NonAAVEDeposit_StaysDeFi(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpDeposit,
		Protocol:      "Compound",
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

// TestClassifier_BridgeRoundTrip covers the bridge-as-swap rule (issue #33,
// ADR-0002). The adapter maps sendToBridge onto OpSend, whose case returns
// transfer_out outright — so without this rule a round-trip bridge would record
// only the outflow and silently drop the asset received in exchange.
func TestClassifier_BridgeRoundTrip(t *testing.T) {
	c := sync.NewClassifier()

	t.Run("different asset back -> swap", func(t *testing.T) {
		// Real shape: 279.158283 USDC out, 0.00362749 cbBTC back, one tx.
		tx := sync.DecodedTransaction{
			OperationType: sync.OpSend,
			ProviderType:  "sendToBridge",
			Transfers: []sync.DecodedTransfer{
				{AssetSymbol: "USDC", Direction: sync.DirectionOut, Amount: big.NewInt(279158283)},
				{AssetSymbol: "cbBTC", Direction: sync.DirectionIn, Amount: big.NewInt(362749)},
			},
		}
		assert.Equal(t, ledger.TxTypeSwap, c.Classify(tx),
			"asset out and a different asset back in one transaction is a trade, "+
				"and booking it as transfer_out would drop the acquisition entirely")
	})

	t.Run("same-asset dust refund -> still transfer_out", func(t *testing.T) {
		// Real shape: most genuine pure-sends refund a sliver of the sent asset.
		tx := sync.DecodedTransaction{
			OperationType: sync.OpSend,
			ProviderType:  "sendToBridge",
			Transfers: []sync.DecodedTransfer{
				{AssetSymbol: "ETH", Direction: sync.DirectionOut, Amount: big.NewInt(8915068809592452)},
				{AssetSymbol: "ETH", Direction: sync.DirectionIn, Amount: big.NewInt(8809592452)},
			},
		}
		assert.Equal(t, ledger.TxTypeTransferOut, c.Classify(tx),
			"getting your own asset back is not a trade — a swap here would fabricate "+
				"a disposal of the asset against itself")
	})

	t.Run("pure send -> transfer_out", func(t *testing.T) {
		tx := sync.DecodedTransaction{
			OperationType: sync.OpSend,
			ProviderType:  "sendToBridge",
			Transfers: []sync.DecodedTransfer{
				{AssetSymbol: "USDC", Direction: sync.DirectionOut, Amount: big.NewInt(1000000)},
			},
		}
		assert.Equal(t, ledger.TxTypeTransferOut, c.Classify(tx))
	})

	t.Run("ordinary send with both directions is untouched", func(t *testing.T) {
		// Not a bridge leg: the rule must not widen to ordinary transfers.
		tx := sync.DecodedTransaction{
			OperationType: sync.OpSend,
			ProviderType:  "sendToken",
			Transfers: []sync.DecodedTransfer{
				{AssetSymbol: "USDC", Direction: sync.DirectionOut, Amount: big.NewInt(1000000)},
				{AssetSymbol: "ETH", Direction: sync.DirectionIn, Amount: big.NewInt(1)},
			},
		}
		assert.Equal(t, ledger.TxTypeTransferOut, c.Classify(tx),
			"the bridge round-trip rule is scoped to provider-identified bridge legs")
	})
}
