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

// TestClassify_LPDeposit_ByLegAction: the transaction is an LP deposit because
// a leg says liquidityAdded, not because a party was named "Uniswap V3". No
// protocol name is set at all here — on real data it is null (#57).
func TestClassify_LPDeposit_ByLegAction(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpDeposit,
		LegActions:    []string{"liquidityAdded", "lpTokensMinted"},
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPDeposit, c.Classify(tx))
}

func TestClassify_LPWithdraw_ByLegAction(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpWithdraw,
		LegActions:    []string{"lpTokenBurned", "liquidityRemoved"},
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionIn, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPWithdraw, c.Classify(tx))
}

func TestClassify_LPMint_ByLegAction(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpMint,
		LegActions:    []string{"liquidityAdded"},
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPDeposit, c.Classify(tx))
}

func TestClassify_LPBurn_ByLegAction(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpBurn,
		LegActions:    []string{"liquidityRemoved"},
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionIn, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPWithdraw, c.Classify(tx))
}

// TestClassify_LPClaimFees_ByLegAction: a claim inside a liquidity pool is an
// LP fee collection, and the pool is identified by the liquidity action on the
// same transaction rather than by a protocol name.
func TestClassify_LPClaimFees_ByLegAction(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpReceive,
		LegActions:    []string{"liquidityRemoved"},
		Acts:          []string{"claim"},
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionIn, AssetSymbol: "USDC", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLPClaimFees, c.Classify(tx))
}

// TestClassify_RewardsReceived_IsDefiClaim pins the boundary the receipt rule
// draws: rewardsReceived is PRINCIPAL, so the reward stays a position — it is
// never dropped like a receipt leg. It books as the protocol-NEUTRAL
// defi_claim because a real claim transaction carries no action naming which
// market paid it (#57).
func TestClassify_RewardsReceived_IsDefiClaim(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpReceive,
		LegActions:    []string{"rewardsReceived", "rewardsReceived"},
		Acts:          []string{"claimRewards", "claim", "rewardsReceived"},
		Transfers: []sync.DecodedTransfer{
			{Direction: sync.DirectionIn, AssetSymbol: "USDC", Amount: big.NewInt(1)},
			{Direction: sync.DirectionIn, AssetSymbol: "ETH", Amount: big.NewInt(2)},
		},
	}
	assert.Equal(t, ledger.TxTypeDefiClaim, c.Classify(tx))
}

// TestClassify_LendingSupply_ByLegAction: identified by the deposited /
// collateralSharesMinted pair the provider stamps, with no protocol name and no
// aToken ticker anywhere — the receipt leg has already been dropped by the time
// the classifier runs, so classification must not depend on seeing it.
func TestClassify_LendingSupply_ByLegAction(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpDeposit,
		LegActions:    []string{"deposited", "collateralSharesMinted"},
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeLendingSupply, c.Classify(tx))
}

// TestClassify_DepositWithoutProtocolAction_StaysDeFi: a deposit whose legs
// carry no lending or liquidity action is a generic defi_deposit. The protocol
// name is deliberately set and deliberately ignored — nothing classifies on it
// any more (#57).
func TestClassify_DepositWithoutProtocolAction_StaysDeFi(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpDeposit,
		Protocol:      "Compound",
		Transfers:     []sync.DecodedTransfer{{Direction: sync.DirectionOut, AssetSymbol: "ETH", Amount: big.NewInt(1)}},
	}
	assert.Equal(t, ledger.TxTypeDefiDeposit, c.Classify(tx))
}

// TestClassify_ProtocolNameAloneDoesNotClassify is the negative of the four
// matchers this change removed: the literal strings they matched on no longer
// route anything. An "Aave" deposit with no lending leg action is a plain
// defi_deposit, and an aToken-shaped ticker does not resurrect the heuristic.
func TestClassify_ProtocolNameAloneDoesNotClassify(t *testing.T) {
	c := sync.NewClassifier()
	for _, protocol := range []string{"Aave", "Aave V3", "AAVE", "Uniswap V3"} {
		tx := sync.DecodedTransaction{
			OperationType: sync.OpDeposit,
			Protocol:      protocol,
			Transfers: []sync.DecodedTransfer{
				{Direction: sync.DirectionOut, AssetSymbol: "aBasWETH", AssetName: "Aave Base WETH", Amount: big.NewInt(1)},
			},
		}
		assert.Equal(t, ledger.TxTypeDefiDeposit, c.Classify(tx), "protocol %q must not classify on its own", protocol)
	}
}

func TestClassify_ReceiveNonClaim_StaysTransferIn(t *testing.T) {
	c := sync.NewClassifier()
	tx := sync.DecodedTransaction{
		OperationType: sync.OpReceive,
		LegActions:    []string{"liquidityRemoved"},
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
