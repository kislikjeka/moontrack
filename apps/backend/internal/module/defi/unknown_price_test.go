package defi_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/ledger"
	"github.com/kislikjeka/moontrack/internal/module/defi"
	"github.com/kislikjeka/moontrack/pkg/logger"
	"github.com/kislikjeka/moontrack/pkg/testasset"
)

// A DeFi leg reaches the handler with no usd_price key at all when the provider
// gave no price: sync's buildDeFiTransferEntry omits the key and enqueues a
// backfill job instead (#74). These tests pin that path down — it panicked in
// production and took the whole backend process with it (#77).
//
// The rule they enforce: an unknown price stays unknown. Never zero, never
// invented from a partial OUT total.

// withdrawData builds a Flux-style withdraw: fUSDC out, USDC in. A transfer map
// carries a usd_price key only when the price is known — omitting it is exactly
// what the sync path does for an unpriced leg.
func withdrawData(walletID uuid.UUID, outPrice, inPrice string) map[string]interface{} {
	out := map[string]interface{}{
		"asset_id":     testasset.ForTicker("fUSDC").String(),
		"asset_symbol": "fUSDC",
		"amount":       "1000000",
		"decimals":     6,
		"direction":    "out",
	}
	if outPrice != "" {
		out["usd_price"] = outPrice
	}

	in := map[string]interface{}{
		"asset_id":     testasset.USDC.String(),
		"asset_symbol": "USDC",
		"amount":       "1000000",
		"decimals":     6,
		"direction":    "in",
	}
	if inPrice != "" {
		in["usd_price"] = inPrice
	}

	return map[string]interface{}{
		"wallet_id":      walletID.String(),
		"tx_hash":        "0xunknownprice",
		"chain_id":       "ethereum",
		"occurred_at":    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		"operation_type": "withdraw",
		"transfers":      []map[string]interface{}{out, in},
	}
}

func withdrawHandler(t *testing.T, ctx context.Context, walletID uuid.UUID) *defi.DeFiWithdrawHandler {
	t.Helper()
	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, uuid.New()), nil)
	return defi.NewDeFiWithdrawHandler(walletRepo, logger.NewDefault("test"))
}

// assertUnknownPrice asserts that an entry carries no price at all, rather than
// a zero standing in for one.
func assertUnknownPrice(t *testing.T, e *ledger.Entry, what string) {
	t.Helper()
	assert.Nil(t, e.USDRate, "%s: unknown rate must stay nil, not zero", what)
	assert.Nil(t, e.USDValue, "%s: unknown value must stay nil, not zero", what)
}

// The panic from #77: an unpriced OUT leg made the running total nil, and
// big.Int.Add on a nil operand segfaulted before any entry was built.
func TestDeFiWithdraw_UnknownOutPrice_NoPanic(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	handler := withdrawHandler(t, ctx, walletID)

	entries, err := handler.Handle(ctx, withdrawData(walletID, "", "100000000"))
	require.NoError(t, err)
	require.Len(t, entries, 4)

	// OUT legs priced the honest way: unknown.
	assertUnknownPrice(t, entries[0], "OUT wallet")
	assertUnknownPrice(t, entries[1], "OUT clearing")

	// The IN leg had its own price and keeps it — an unknown OUT total must not
	// erase a price that was actually known.
	require.NotNil(t, entries[2].USDRate)
	assert.Equal(t, "100000000", entries[2].USDRate.String())

	assertEntriesBalanced(t, entries)
}

// The second nil receiver on the same line: usdRate.Sign() with an unpriced IN
// leg. Here the OUT side is priced, so the fallback legitimately fires.
func TestDeFiWithdraw_UnknownInPrice_DerivedFromOut(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	handler := withdrawHandler(t, ctx, walletID)

	entries, err := handler.Handle(ctx, withdrawData(walletID, "100000000", ""))
	require.NoError(t, err)
	require.Len(t, entries, 4)

	// 1 fUSDC out at $1.00 → the 1 USDC in is worth $1.00 too.
	require.NotNil(t, entries[2].USDRate, "a known OUT total must supply the IN rate")
	assert.Equal(t, "100000000", entries[2].USDRate.String())
	require.NotNil(t, entries[2].USDValue)
	assert.Equal(t, "100000000", entries[2].USDValue.String())

	assertEntriesBalanced(t, entries)
}

// The semantic core of the fix: when the OUT total is unknown there is nothing
// to derive from, so the IN price must remain unknown. Substituting zero here
// would recreate the #74 lie — a lot marked resolved with a cost basis of 0.
func TestDeFiWithdraw_BothPricesUnknown_StayUnknown(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	handler := withdrawHandler(t, ctx, walletID)

	entries, err := handler.Handle(ctx, withdrawData(walletID, "", ""))
	require.NoError(t, err)
	require.Len(t, entries, 4)

	for i, e := range entries {
		assertUnknownPrice(t, e, "entry "+string(rune('0'+i)))
	}

	assertEntriesBalanced(t, entries)
}

// A partial OUT total is not a total. With one OUT leg priced and one unpriced,
// the sum understates what left the wallet, so using it would invent an IN rate
// that is confidently wrong. The whole total is unknown and the fallback stays
// silent.
func TestDeFiWithdraw_PartiallyPricedOut_DoesNotDeriveInPrice(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	handler := withdrawHandler(t, ctx, walletID)

	data := withdrawData(walletID, "100000000", "")
	transfers := data["transfers"].([]map[string]interface{})
	// Second OUT leg, deliberately unpriced.
	data["transfers"] = append(transfers, map[string]interface{}{
		"asset_id":     testasset.ForTicker("fDAI").String(),
		"asset_symbol": "fDAI",
		"amount":       "2000000",
		"decimals":     6,
		"direction":    "out",
	})

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 6)

	// Entries 4 and 5 are the IN pair (two OUT pairs precede them).
	assertUnknownPrice(t, entries[4], "IN wallet")
	assertUnknownPrice(t, entries[5], "IN clearing")

	assertEntriesBalanced(t, entries)
}

// An explicit zero price is a real datum, not an absence, and remains eligible
// for the fallback — that behaviour predates #74 and is preserved.
func TestDeFiWithdraw_ZeroInPrice_StillDerivedFromOut(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	handler := withdrawHandler(t, ctx, walletID)

	entries, err := handler.Handle(ctx, withdrawData(walletID, "100000000", "0"))
	require.NoError(t, err)
	require.Len(t, entries, 4)

	require.NotNil(t, entries[2].USDRate)
	assert.Equal(t, "100000000", entries[2].USDRate.String())
}

// Gas is the other rate on this path: an unpriced fee asset must not panic
// either, and its entries carry an unknown value rather than a zero one.
func TestDeFiWithdraw_UnknownFeePrice_NoPanic(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()
	handler := withdrawHandler(t, ctx, walletID)

	data := withdrawData(walletID, "100000000", "100000000")
	data["fee_asset"] = testasset.ETH.String()
	data["fee_amount"] = "50000000000000"
	data["fee_decimals"] = 18
	// fee_usd_price deliberately absent.

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 6)

	assertUnknownPrice(t, entries[4], "gas debit")
	assertUnknownPrice(t, entries[5], "gas credit")
}

// Deposits share generateSwapLikeEntries, so the same nil rate reaches them by
// the same route.
func TestDeFiDeposit_UnknownPrices_NoPanic(t *testing.T) {
	ctx := context.Background()
	walletID := uuid.New()

	walletRepo := new(MockWalletRepository)
	walletRepo.On("GetByID", ctx, walletID).Return(testWallet(walletID, uuid.New()), nil)
	handler := defi.NewDeFiDepositHandler(walletRepo, logger.NewDefault("test"))

	data := withdrawData(walletID, "", "")
	data["operation_type"] = "deposit"

	entries, err := handler.Handle(ctx, data)
	require.NoError(t, err)
	require.Len(t, entries, 4)

	for i, e := range entries {
		assertUnknownPrice(t, e, "entry "+string(rune('0'+i)))
	}

	assertEntriesBalanced(t, entries)
}
