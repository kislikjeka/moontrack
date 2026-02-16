package sync_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

func TestDecodedTransaction_CompileCheck(t *testing.T) {
	tx := sync.DecodedTransaction{
		ID:            "tx-1",
		TxHash:        "0xabc",
		ChainID:       1,
		OperationType: sync.OpTrade,
		Protocol:      "Uniswap V3",
		Transfers: []sync.DecodedTransfer{
			{
				AssetSymbol:     "ETH",
				ContractAddress: "",
				Decimals:        18,
				Amount:          big.NewInt(1000000000000000000),
				Direction:       sync.DirectionOut,
				Sender:          "0xsender",
				Recipient:       "0xrecipient",
				USDPrice:        big.NewInt(350000000000), // $3500.00
			},
		},
		Fee: &sync.DecodedFee{
			AssetSymbol: "ETH",
			Amount:      big.NewInt(21000000000000),
			Decimals:    18,
			USDPrice:    big.NewInt(7350000), // $0.0735
		},
		MinedAt: time.Now(),
		Status:  "confirmed",
	}

	assert.Equal(t, "tx-1", tx.ID)
	assert.Equal(t, sync.OpTrade, tx.OperationType)
	assert.Len(t, tx.Transfers, 1)
	assert.NotNil(t, tx.Fee)
}

func TestOperationType_Constants(t *testing.T) {
	ops := []sync.OperationType{
		sync.OpTrade,
		sync.OpDeposit,
		sync.OpWithdraw,
		sync.OpClaim,
		sync.OpReceive,
		sync.OpSend,
		sync.OpExecute,
		sync.OpApprove,
		sync.OpMint,
		sync.OpBurn,
	}

	// Verify all constants are unique non-empty strings
	seen := make(map[sync.OperationType]bool)
	for _, op := range ops {
		assert.NotEmpty(t, string(op))
		assert.False(t, seen[op], "duplicate operation type: %s", op)
		seen[op] = true
	}
	assert.Len(t, seen, 10)
}

func TestDecodedTransfer_NilUSDPrice(t *testing.T) {
	transfer := sync.DecodedTransfer{
		AssetSymbol: "UNKNOWN",
		Amount:      big.NewInt(100),
		Direction:   sync.DirectionIn,
		USDPrice:    nil,
	}
	assert.Nil(t, transfer.USDPrice)
}

func TestDecodedFee_Fields(t *testing.T) {
	fee := sync.DecodedFee{
		AssetSymbol: "ETH",
		Amount:      big.NewInt(21000000000000),
		Decimals:    18,
		USDPrice:    big.NewInt(7350000),
	}

	assert.Equal(t, "ETH", fee.AssetSymbol)
	assert.Equal(t, 0, fee.Amount.Cmp(big.NewInt(21000000000000)))
	assert.Equal(t, 18, fee.Decimals)
}
