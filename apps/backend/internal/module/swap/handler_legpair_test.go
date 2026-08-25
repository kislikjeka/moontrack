package swap_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/swap"
	"github.com/kislikjeka/moontrack/internal/platform/wallet"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/money"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// TestSwapHandler_NoLegPairMarker: a swap has no leg pair, by definition.
//
// #84 gave the movements that carry a cost basis across — internal transfer,
// lending supply and withdraw — an explicit pair marker, because the tax-lot
// hook can no longer infer the pairing from asset equality. A swap is not one of
// them: its two sides are different economic assets, and realizing the gain
// there is the intended accounting, not a defect to route around.
//
// Marking a swap would tell the hook to carry the disposed asset's basis onto
// the acquired one, erasing exactly the realized PnL the swap exists to record.
// This pins the absence so it stays deliberate.
func TestSwapHandler_NoLegPairMarker(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	userID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(&wallet.Wallet{
		ID:      walletID,
		UserID:  userID,
		Address: "0x1234567890123456789012345678901234567890",
	}, nil)

	handler := swap.NewSwapHandler(walletRepo, logger.NewDefault("test"))

	data := map[string]interface{}{
		"wallet_id":   walletID.String(),
		"tx_hash":     "0xswaplegpair",
		"chain_id":    "ethereum",
		"occurred_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		"protocol":    "uniswap_v3",
		"transfers_out": []map[string]interface{}{{
			"asset_id":     testasset.ETH.String(),
			"asset_symbol": "ETH",
			"amount":       money.NewBigIntFromInt64(1000000000000000000).String(),
			"decimals":     18,
			"usd_price":    "200000000000",
			"sender":       "0x1234",
			"recipient":    "0xdex",
		}},
		"transfers_in": []map[string]interface{}{{
			"asset_id":         testasset.USDC.String(),
			"asset_symbol":     "USDC",
			"amount":           money.NewBigIntFromInt64(2000000000).String(),
			"decimals":         6,
			"usd_price":        "100000000",
			"contract_address": "0xusdc",
			"sender":           "0xdex",
			"recipient":        "0x1234",
		}},
	}

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, e := range entries {
		assert.NotContains(t, e.Metadata, ledger.MetaLegPair,
			"a swap's sides are two economic assets, not two legs of one movement; a marker here "+
				"would carry the disposed basis onto the acquisition and erase the realized gain")
	}
}
