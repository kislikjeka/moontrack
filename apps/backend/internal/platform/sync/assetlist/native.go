// Package assetlist carries the OFFLINE half of the known-asset filter (#58):
// the built-in list of major coins and the native coin of every chain.
//
// It has no dependencies beyond the standard library and makes no network calls
// — that is the whole point. Level 1 of the resolve has to answer instantly and
// identically on every machine, so it is compiled in.
package assetlist

import "strings"

// NativeContract mirrors sync.NativeContract. It is duplicated rather than
// imported because assetlist sits BELOW sync in the dependency order — sync
// imports assetlist, so the arrow cannot point back. The value is a single
// literal pinned by TestNativeSentinelMatchesSync.
const NativeContract = "native"

// nativeSymbols maps a chain to the ticker of its native coin.
//
// This table is why the filter does not kill the balance. A native leg carries
// no contract, so it can never appear in a token list; without knownness by
// construction every native leg — the largest position in most wallets, and
// ALL gas — would resolve as unknown and drop out of the ledger.
//
// Knownness is granted to the pair (chain, native) WITH A SYMBOL CHECK, never to
// "any leg with a blank contract". The distinction is load-bearing: the provider
// really does emit legs with an unrecognisable symbol and zero decimals and no
// contract at all, and the pre-#56 predicate ("native is anything not starting
// with 0x") merged them into the chain's real native coin. Requiring the symbol
// to match keeps a junk leg junk.
var nativeSymbols = map[string]string{
	"ethereum":            "ETH",
	"base":                "ETH",
	"arbitrum":            "ETH",
	"optimism":            "ETH",
	"polygon":             "POL",
	"avalanche":           "AVAX",
	"binance-smart-chain": "BNB",
}

// NativeSymbol returns the expected native ticker of a chain, and whether the
// chain is known at all.
func NativeSymbol(chain string) (string, bool) {
	s, ok := nativeSymbols[strings.ToLower(strings.TrimSpace(chain))]
	return s, ok
}

// IsNative reports whether (chain, contract, symbol) is that chain's native coin.
//
// All three have to agree. A leg claiming the native contract on a chain we do
// not know, or carrying a symbol that is not that chain's coin, is NOT native —
// it is an unknown asset that happens to have no contract, and it is treated as
// one.
func IsNative(chain, contract, symbol string) bool {
	if !strings.EqualFold(strings.TrimSpace(contract), NativeContract) {
		return false
	}
	want, ok := NativeSymbol(chain)
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(symbol), want)
}
