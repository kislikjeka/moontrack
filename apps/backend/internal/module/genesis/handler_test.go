package genesis_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/genesis"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// genesisData builds a genesis balance payload. usd_rate is present only when
// the rate is known — an absent key is how sync expresses "no price yet" (#74).
func genesisData(walletID uuid.UUID, usdRate string) map[string]interface{} {
	data := map[string]interface{}{
		"wallet_id":    walletID.String(),
		"asset_id":     testasset.USDC.String(),
		"asset_symbol": "USDC",
		"chain_id":     "ethereum",
		"amount":       "1000000",
		"decimals":     6,
		"occurred_at":  time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
	}
	if usdRate != "" {
		data["usd_rate"] = usdRate
	}
	return data
}

func TestGenesisHandler_Type(t *testing.T) {
	h := genesis.NewHandler(logger.NewDefault("test"))
	assert.Equal(t, ledger.TxTypeGenesisBalance, h.Type())
}

func TestGenesisHandler_KnownRate_ComputesValue(t *testing.T) {
	h := genesis.NewHandler(logger.NewDefault("test"))

	entries, err := h.Handle(context.Background(), genesisData(uuid.New(), "100000000"))
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// 1 USDC (6 decimals) at $1.00 → a USD value of $1.00 scaled by 10^8.
	for _, e := range entries {
		require.NotNil(t, e.USDRate)
		assert.Equal(t, "100000000", e.USDRate.String())
		require.NotNil(t, e.USDValue)
		assert.Equal(t, "100000000", e.USDValue.String())
	}
}

// The genesis twin of #77: an absent rate reached big.Int.Mul as a nil operand
// and segfaulted. It must now produce an honest unknown instead.
func TestGenesisHandler_UnknownRate_NoPanicAndStaysUnknown(t *testing.T) {
	h := genesis.NewHandler(logger.NewDefault("test"))

	entries, err := h.Handle(context.Background(), genesisData(uuid.New(), ""))
	require.NoError(t, err)
	require.Len(t, entries, 2)

	for _, e := range entries {
		assert.Nil(t, e.USDRate, "unknown rate must stay nil, not zero")
		assert.Nil(t, e.USDValue, "unknown value must stay nil, not zero")
	}
}

// Zero decimals used to skip the divisor entirely; 10^0 is 1, so routing
// through money.CalcUSDValue keeps the arithmetic identical.
func TestGenesisHandler_ZeroDecimals_ValueUnscaled(t *testing.T) {
	h := genesis.NewHandler(logger.NewDefault("test"))

	data := genesisData(uuid.New(), "100000000")
	data["decimals"] = 0
	data["amount"] = "3"

	entries, err := h.Handle(context.Background(), data)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	require.NotNil(t, entries[0].USDValue)
	assert.Equal(t, "300000000", entries[0].USDValue.String())
}

func TestGenesisHandler_Validate_MissingFields(t *testing.T) {
	h := genesis.NewHandler(logger.NewDefault("test"))
	ctx := context.Background()
	walletID := uuid.New()

	tests := []struct {
		name   string
		mutate func(map[string]interface{})
	}{
		{"missing wallet_id", func(d map[string]interface{}) { delete(d, "wallet_id") }},
		{"missing asset_id", func(d map[string]interface{}) { delete(d, "asset_id") }},
		{"missing chain_id", func(d map[string]interface{}) { delete(d, "chain_id") }},
		{"zero amount", func(d map[string]interface{}) { d["amount"] = "0" }},
		{"missing amount", func(d map[string]interface{}) { delete(d, "amount") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := genesisData(walletID, "100000000")
			tt.mutate(data)
			_, err := h.Handle(ctx, data)
			assert.Error(t, err)
		})
	}
}
