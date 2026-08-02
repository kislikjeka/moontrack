package testasset

import (
	"github.com/google/uuid"
)

// Entry pairs a fixed id with its ticker.
type Entry struct {
	ID     uuid.UUID
	Symbol string
}

// All returns every fixed id in this package paired with its ticker.
//
// Integration tests need it because accounts, entries, account_balances and
// tax_lots all carry a foreign key into asset_registry since #59: a test that
// books an entry against testasset.ETH needs a registry row with THAT id to
// exist first. The registry's own Resolve mints ids server-side and cannot be
// told to use a specific one, so integration tests insert these directly.
//
// Keeping the list here rather than in each test package is what stops a seeder
// from drifting from the constants — adding an id above without adding it here
// surfaces as a foreign-key violation with no obvious cause.
func All() []Entry {
	return []Entry{
		{ETH, "ETH"},
		{WETH, "WETH"},
		{USDC, "USDC"},
		{USDT, "USDT"},
		{DAI, "DAI"},
		{BTC, "BTC"},
		{WBTC, "WBTC"},
		{AAVE, "AAVE"},
		{MATIC, "MATIC"},
		{BNB, "BNB"},
		{CRV, "CRV"},
		{UNI, "UNI"},
		{ARB, "ARB"},
		{LINK, "LINK"},
	}
}
