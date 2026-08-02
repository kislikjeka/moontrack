package noves

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// loadBalances reads a testdata JSON array into []BalanceItem.
func loadBalances(t *testing.T, name string) []BalanceItem {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "read fixture %s", name)
	var items []BalanceItem
	require.NoError(t, json.Unmarshal(data, &items), "unmarshal fixture %s", name)
	return items
}

func TestPositions_InterfaceCompliance(t *testing.T) {
	var _ sync.PositionDataProvider = (*SyncAdapter)(nil)
}

// TestConvertBalance_BaseUnits asserts each fixture balance converts to exact
// base units at its own decimals scale, on the canonical chain slug.
func TestConvertBalance_BaseUnits(t *testing.T) {
	items := loadBalances(t, "balances.json")
	require.Len(t, items, 3)

	bySymbol := map[string]sync.OnChainPosition{}
	for _, item := range items {
		pos, ok := convertBalance(item, "ethereum")
		require.True(t, ok, "balance %s should convert", item.Token.Symbol)
		bySymbol[pos.AssetSymbol] = pos
	}

	// Native ETH: 1.075192143935849059 × 10^18 → exact base units, contract is
	// the native sentinel (#56).
	eth := bySymbol["ETH"]
	assert.Equal(t, "ethereum", eth.ChainID)
	assert.Equal(t, 18, eth.Decimals)
	assert.Equal(t, sync.NativeContract, eth.ContractAddress, "native coin carries the native sentinel")
	assert.Equal(t, "1075192143935849059", eth.Quantity.String())

	// USDC: 7854.743084 × 10^6, contract lowercased.
	usdc := bySymbol["USDC"]
	assert.Equal(t, 6, usdc.Decimals)
	assert.Equal(t, "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", usdc.ContractAddress)
	assert.Equal(t, "7854743084", usdc.Quantity.String())

	// cbBTC: 0.07685618 × 10^8.
	cbbtc := bySymbol["cbBTC"]
	assert.Equal(t, 8, cbbtc.Decimals)
	assert.Equal(t, "7685618", cbbtc.Quantity.String())
}

// TestConvertBalance_DropsZeroAndNil asserts zero-balance and tokenless items
// are dropped (the reconciler only acts on positive quantities).
func TestConvertBalance_DropsZeroAndNil(t *testing.T) {
	_, ok := convertBalance(BalanceItem{Balance: "0", Token: &BalanceToken{Symbol: "X", Decimals: 18}}, "base")
	assert.False(t, ok, "zero balance dropped")

	_, ok = convertBalance(BalanceItem{Balance: "1.0", Token: nil}, "base")
	assert.False(t, ok, "nil token dropped")
}

// TestGetPositions_SingleChain drives the adapter against a fake Noves balances
// endpoint for one chain and asserts it maps the domain slug to the Noves short
// slug, stamping the canonical chain slug on each converted position. The
// Reconciler owns the fan-out over the wallet's chain set (issue #27); the
// adapter handles exactly the chain it is asked for.
func TestGetPositions_SingleChain(t *testing.T) {
	balances := loadBalances(t, "balances.json")

	var hitChains []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// URL shape: /evm/{chain}/tokens/balancesOf/{addr}. Serve balances only
		// for the "eth" chain, empty array otherwise — proves per-chain slugging.
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/evm/eth/") {
			hitChains = append(hitChains, "eth")
			_ = json.NewEncoder(w).Encode(balances)
			return
		}
		_ = json.NewEncoder(w).Encode([]BalanceItem{})
	}))
	defer server.Close()

	client := NewClient("key", logger.New("development", os.NewFile(0, os.DevNull)))
	client.SetBaseURL(server.URL)
	adapter := NewSyncAdapter(client)

	positions, err := adapter.GetPositions(context.Background(), "0xabc", "ethereum")
	require.NoError(t, err)

	// The three fixture balances come back exactly once, on the ethereum chain.
	require.Len(t, positions, 3)
	for _, p := range positions {
		assert.Equal(t, "ethereum", p.ChainID, "eth balances mapped to canonical 'ethereum'")
	}
	assert.Contains(t, hitChains, "eth")
}

// TestGetPositions_UnmappedChain asserts a non-Compatible chain (no Noves slug)
// yields no positions and no error, so the reconciler simply gets nothing for it.
func TestGetPositions_UnmappedChain(t *testing.T) {
	client := NewClient("key", logger.New("development", os.NewFile(0, os.DevNull)))
	adapter := NewSyncAdapter(client)

	positions, err := adapter.GetPositions(context.Background(), "0xabc", "not-a-real-chain")
	require.NoError(t, err)
	assert.Empty(t, positions)
}

// TestGetBalances_TooManyTokensError asserts the `{detail}` error envelope the
// balances endpoint returns for degenerate wallets is surfaced as an error, not
// swallowed as an empty balance set (which would fabricate spurious genesis).
func TestGetBalances_TooManyTokensError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"detail": `{"message":"Wallet 0x... has too many ERC20 token balances for 0x1 chain"}`,
		})
	}))
	defer server.Close()

	client := NewClient("key", logger.New("development", os.NewFile(0, os.DevNull)))
	client.SetBaseURL(server.URL)

	_, err := client.GetBalances(context.Background(), "eth", "0xabc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many ERC20")
}
