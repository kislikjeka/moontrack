package assetlist

import (
	"strings"
	"testing"
)

// TestBuiltinList_IsNotEmpty guards against the generated list silently
// vanishing — an empty builtin map would compile and pass every other test while
// quietly demoting every major coin to level 2, and the symptom would show up
// days later as missing balance rather than as a build failure.
func TestBuiltinList_IsNotEmpty(t *testing.T) {
	if Size() == 0 {
		t.Fatal("the generated built-in list is empty; run `go generate ./internal/platform/sync/assetlist`")
	}
}

// TestBuiltinList_KnowsRealCoinsAndNotTheForgeries checks the generated list
// against the ACTUAL contracts from the #37 measurement over 393 real raws.
//
// The point is not that some list exists but that it separates the exact pairs
// the attack turns on: Circle's USDC on base is in it, and the four homoglyph
// forgeries that mirrored real sends down to the amount are not.
func TestBuiltinList_KnowsRealCoinsAndNotTheForgeries(t *testing.T) {
	known := []struct {
		name     string
		chain    string
		contract string
	}{
		{"real USDC on base", "base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"},
		{"cbBTC on base", "base", "0xcbb7c0000ab88b473b1f5afd9ef808440eed33bf"},
	}
	for _, tc := range known {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := Lookup(tc.chain, tc.contract); !ok {
				t.Errorf("%s must be in the built-in list", tc.name)
			}
		})
	}

	// The four homoglyph forgeries of USDC from the measured history. The issue
	// records their address PREFIXES, so the assertion is made on the prefix:
	// no entry in the generated list may begin with any of them. That is the
	// stronger claim anyway — it holds for every address sharing the prefix, not
	// just one reconstructed full address.
	forgeryPrefixes := []struct {
		name   string
		prefix string
	}{
		{"UЅDС (Cyrillic Ѕ, С)", "0xeb9caafc"},
		{"ÚSDС", "0xa1adabc4"},
		{"UЅDC", "0xede04208"},
		{"ꓴꓢꓓC (Lisu letters)", "0x72dcc25e"},
	}
	for _, tc := range forgeryPrefixes {
		t.Run(tc.name, func(t *testing.T) {
			for key, sym := range builtin {
				if strings.HasPrefix(key.Contract, tc.prefix) {
					t.Errorf("forgery %s must NOT be in the built-in list (found %s as %q)",
						tc.name, key.Contract, sym)
				}
			}
		})
	}
}

// TestLookup_NormalizesCasing pins that a checksum-cased address still hits.
// Providers are inconsistent about EVM address casing, and a lookup that missed
// on case would demote real coins to level 2 at random.
func TestLookup_NormalizesCasing(t *testing.T) {
	const checksummed = "0x833589FCD6EDB6E08F4C7C32D4F71B54BDA02913"
	if _, ok := Lookup("base", checksummed); !ok {
		t.Error("a checksum-cased address must resolve against the lowercased list")
	}
	if _, ok := Lookup("BASE", checksummed); !ok {
		t.Error("chain slug casing must be normalized too")
	}
}

// TestIsNative_RequiresSymbolAgreement is the edge case the ticket names
// explicitly. Nativeness is granted to (chain, native) WITH the symbol checked,
// never to "any leg with a blank contract" — otherwise a junk leg carrying an
// unrecognisable symbol and zero decimals would be merged with the chain's real
// coin, which is the silent failure the whole epic exists to remove.
func TestIsNative_RequiresSymbolAgreement(t *testing.T) {
	tests := []struct {
		name     string
		chain    string
		contract string
		symbol   string
		want     bool
	}{
		{"ETH on ethereum", "ethereum", NativeContract, "ETH", true},
		{"ETH on base", "base", NativeContract, "ETH", true},
		{"lowercase symbol still matches", "base", NativeContract, "eth", true},
		{"UNKN is not native", "ethereum", NativeContract, "UNKN", false},
		{"blank symbol is not native", "ethereum", NativeContract, "", false},
		{"wrong chain's coin", "base", NativeContract, "AVAX", false},
		{"unknown chain has no native", "not-a-chain", NativeContract, "ETH", false},
		{"a token contract is never native", "base", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", "USDC", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNative(tc.chain, tc.contract, tc.symbol); got != tc.want {
				t.Errorf("IsNative(%q, %q, %q) = %v, want %v",
					tc.chain, tc.contract, tc.symbol, got, tc.want)
			}
		})
	}
}

// TestNativeSymbol_CoversEveryEnabledChain guards the rule that makes the filter
// safe to turn on at all: the measured history has 104 native legs — the largest
// position and ALL gas — and every one of them is judged by this table. A chain
// missing here would drop its native coin out of the ledger entirely.
func TestNativeSymbol_CoversEveryEnabledChain(t *testing.T) {
	// The Enabled set from CONTEXT.md.
	for _, chain := range []string{"ethereum", "base", "arbitrum"} {
		if _, ok := NativeSymbol(chain); !ok {
			t.Errorf("enabled chain %q has no native coin registered", chain)
		}
	}
}
