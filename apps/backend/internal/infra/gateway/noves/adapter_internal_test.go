package noves

import (
	"testing"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

func TestDomainToNovesChain(t *testing.T) {
	tests := []struct {
		domain string
		want   string
		ok     bool
	}{
		{"ethereum", "eth", true},
		{"binance-smart-chain", "bsc", true},
		{"base", "base", true},
		{"arbitrum", "arbitrum", true},
		{"polygon", "polygon", true},
		{"optimism", "optimism", true},
		{"avalanche", "avalanche", true},
		{"solana", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := domainToNovesChain(tt.domain)
		if ok != tt.ok || got != tt.want {
			t.Errorf("domainToNovesChain(%q) = (%q, %v), want (%q, %v)", tt.domain, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNovesToDomainChain_Roundtrip(t *testing.T) {
	for domain := range domainToNoves {
		noves, ok := domainToNovesChain(domain)
		if !ok {
			t.Fatalf("domainToNovesChain(%q) not ok", domain)
		}
		back, ok := novesToDomainChain(noves)
		if !ok {
			t.Fatalf("novesToDomainChain(%q) not ok", noves)
		}
		if back != domain {
			t.Errorf("roundtrip %q -> %q -> %q", domain, noves, back)
		}
	}
	// ethereum specifically must roundtrip through the short slug.
	if noves, _ := domainToNovesChain("ethereum"); noves != "eth" {
		t.Fatalf("ethereum should map to eth, got %q", noves)
	}
	if back, _ := novesToDomainChain("eth"); back != "ethereum" {
		t.Fatalf("eth should map back to ethereum, got %q", back)
	}
}

func TestIsNativeAddress(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"ETH", true},
		{"", true},
		{"MATIC", true},
		{"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", false},
		{"0xCBB7C0000AB88B473B1F5AFD9EF808440EED33BF", false},
	}
	for _, tt := range tests {
		if got := isNativeAddress(tt.address); got != tt.want {
			t.Errorf("isNativeAddress(%q) = %v, want %v", tt.address, got, tt.want)
		}
	}
}

// TestNormalizeContract pins the adapter's half of the (chain, contract)
// identity contract (issue #56): the native leg is translated into the literal
// sentinel, a leg carrying a real address keeps that address, and casing is
// normalized consistently so one contract cannot yield two identities.
func TestNormalizeContract(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		// Native legs. Noves spells native as a symbol-as-address sentinel, and
		// each spelling must land on the ONE literal — an empty contract here
		// used to mean native, and that ambiguity is what #56 removes.
		{"native symbol-as-address", "ETH", sync.NativeContract},
		{"native on a non-eth chain", "MATIC", sync.NativeContract},
		{"native as empty address", "", sync.NativeContract},

		// A real address survives intact, only lowercased.
		{"checksummed address is lowercased", "0x833589FCD6EDB6E08F4C7C32D4F71B54BDA02913", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"},
		{"already-lowercase address is unchanged", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"},
		{"mixed-case address is lowercased", "0xCBB7C0000AB88B473B1F5AFD9EF808440EED33BF", "0xcbb7c0000ab88b473b1f5afd9ef808440eed33bf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeContract(tt.address); got != tt.want {
				t.Errorf("normalizeContract(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

// TestNormalizeContract_CasingIsConsistent is the identity-level statement the
// case-by-case table cannot make: the SAME contract written three ways must
// normalize to ONE value. Were it not so, one token would occupy several rows
// of a registry keyed on (chain, contract) — each with its own UUID and its own
// tax lots — which is the very splitting the composite key exists to prevent.
func TestNormalizeContract_CasingIsConsistent(t *testing.T) {
	spellings := []string{
		"0x833589FCD6EDB6E08F4C7C32D4F71B54BDA02913",
		"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913",
		"0x833589FcD6EdB6E08f4c7C32D4f71b54bdA02913",
	}

	first := normalizeContract(spellings[0])
	for _, s := range spellings[1:] {
		if got := normalizeContract(s); got != first {
			t.Errorf("normalizeContract(%q) = %q, want %q — the same contract must yield one identity", s, got, first)
		}
	}
}

func TestFractionalDigits(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"120", 0},
		{"120.5", 1},
		{"1.23456789", 8},
		{"0.000002387988852403", 18},
		{"1.", 0},
	}
	for _, tt := range tests {
		if got := fractionalDigits(tt.in); got != tt.want {
			t.Errorf("fractionalDigits(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestAmountToBaseUnits_ExactOrFlag(t *testing.T) {
	// Exact: 6 decimals, 6 fractional digits — no flag.
	amount, review := amountToBaseUnits("120.559701", 6, "USDC")
	if review != "" {
		t.Errorf("exact conversion should not flag, got %q", review)
	}
	if amount.String() != "120559701" {
		t.Errorf("got %s, want 120559701", amount.String())
	}

	// Loss: 6 decimals, 8 fractional digits — must flag but still return value.
	amount, review = amountToBaseUnits("1.23456789", 6, "USDC")
	if review == "" {
		t.Error("truncating conversion must flag with a review reason")
	}
	if amount.String() != "1234567" {
		t.Errorf("truncated amount = %s, want 1234567", amount.String())
	}

	// decimals=0 identity — no flag, no panic.
	amount, review = amountToBaseUnits("42", 0, "FOO")
	if review != "" {
		t.Errorf("decimals=0 integer should not flag, got %q", review)
	}
	if amount.String() != "42" {
		t.Errorf("got %s, want 42", amount.String())
	}
}

func TestMapOperationType(t *testing.T) {
	tests := []struct {
		novesType string
		want      string
	}{
		{"swap", "trade"},
		{"depositCollateral", "deposit"},
		{"addLiquidity", "deposit"},
		{"removeLiquidity", "withdraw"},
		{"withdrawCollateral", "withdraw"},
		// claimRewards maps to receive: the classifier's claim paths (LP fee
		// claim, lending reward claim) fire on OpReceive + a "claim" act.
		{"claimRewards", "receive"},
		{"receiveToken", "receive"},
		{"receiveFromBridge", "receive"},
		{"sendToken", "send"},
		{"sendToBridge", "send"},
		{"approveToken", "approve"},
		{"unclassified", "execute"},
		{"someUnknownFutureType", "execute"},
	}
	for _, tt := range tests {
		if got := string(mapOperationType(tt.novesType)); got != tt.want {
			t.Errorf("mapOperationType(%q) = %q, want %q", tt.novesType, got, tt.want)
		}
	}
}

// TestIsUnclassifiedType pins the boundary of the "provider could not classify
// it" bucket (issue #30). mapOperationType collapses several distinct Noves
// types onto OpExecute, so OpExecute alone cannot tell an unknown shape from a
// known one — this predicate is what carries the distinction across the port.
func TestIsUnclassifiedType(t *testing.T) {
	tests := []struct {
		novesType string
		want      bool
	}{
		// Provider admits it does not know what happened.
		{"unclassified", true},
		{"unverifiedContract", true},
		// A type this adapter has no mapping for is, to us, equally unknown.
		{"someUnknownFutureType", true},
		// Types the provider DID classify — including the ones that also land
		// on OpExecute-adjacent paths.
		{"swap", false},
		{"sendToBridge", false},
		{"receiveFromBridge", false},
		{"claimRewards", false},
		{"approveToken", false},
		// A failed transaction is classified: it carries its own Status and is
		// skipped by the TxBuilder before classification ever runs.
		{"failed", false},
	}
	for _, tt := range tests {
		if got := isUnclassifiedType(tt.novesType); got != tt.want {
			t.Errorf("isUnclassifiedType(%q) = %v, want %v", tt.novesType, got, tt.want)
		}
	}
}
