package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/kislikjeka/moontrack/internal/platform/asset"
)

func TestNormalizeContractAddress_EVM_Trimming(t *testing.T) {
	const canonical = "0xabcdef0123456789abcdef0123456789abcdef01"

	cases := []string{
		canonical,
		"  " + canonical + "  ",
		strings.ToUpper(canonical),
		" 0xABCDEF0123456789ABCDEF0123456789ABCDEF01 ",
	}
	for _, in := range cases {
		got, err := normalizeContractAddress("ethereum", in)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", in, err)
		}
		if got != canonical {
			t.Fatalf("%q normalized to %q, want %q", in, got, canonical)
		}
	}
}

func TestNormalizeContractAddress_EVM_InvalidShape(t *testing.T) {
	cases := []string{
		"0xghij000000000000000000000000000000000000", // non-hex
		"0xabc",                                      // too short
		"1234567890abcdef1234567890abcdef12345678",   // missing 0x prefix
		"0x",                                         // just prefix
	}
	for _, in := range cases {
		_, err := normalizeContractAddress("ethereum", in)
		if !errors.Is(err, asset.ErrInvalidContractAddress) {
			t.Fatalf("%q: expected ErrInvalidContractAddress, got %v", in, err)
		}
	}
}

func TestNormalizeContractAddress_AllEVMChains(t *testing.T) {
	chains := []string{"ethereum", "arbitrum", "optimism", "base", "polygon",
		"bnb-chain", "avalanche", "linea", "zksync", "scroll"}
	for _, chain := range chains {
		_, err := normalizeContractAddress(chain, "0xabcdef0123456789abcdef0123456789abcdef01")
		if err != nil {
			t.Fatalf("chain %q: unexpected error: %v", chain, err)
		}
		_, err = normalizeContractAddress(chain, "bad")
		if !errors.Is(err, asset.ErrInvalidContractAddress) {
			t.Fatalf("chain %q: expected ErrInvalidContractAddress for bad addr, got %v", chain, err)
		}
	}
}

func TestNormalizeContractAddress_NonEVMChains(t *testing.T) {
	// Solana / unknown chains only trim+lower; no shape check.
	got, err := normalizeContractAddress("solana", "  SomeBase58Addr123  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "somebase58addr123" {
		t.Fatalf("got %q, want %q", got, "somebase58addr123")
	}

	got, err = normalizeContractAddress("cosmos-hub", " ADDR ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "addr" {
		t.Fatalf("got %q, want %q", got, "addr")
	}
}

func TestNormalizeContractAddress_Empty(t *testing.T) {
	got, err := normalizeContractAddress("ethereum", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}

	got, err = normalizeContractAddress("ethereum", "   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestSanitizeProviderField_CapsAndStripsControl(t *testing.T) {
	in := "foo\nbar\tbaz\x00qux"
	got := sanitizeProviderField(in, 100)
	for i := 0; i < len(got); i++ {
		if got[i] < 0x20 {
			t.Fatalf("control byte 0x%02x left in %q", got[i], got)
		}
	}
}

func TestSanitizeProviderField_Truncate10kSymbol(t *testing.T) {
	in := strings.Repeat("A", 10000)
	got := sanitizeProviderField(in, symbolCapBytes)
	if len(got) != symbolCapBytes {
		t.Fatalf("expected length %d, got %d", symbolCapBytes, len(got))
	}
}

func TestSanitizeProviderField_TruncateLongName(t *testing.T) {
	in := strings.Repeat("N", 10000)
	got := sanitizeProviderField(in, nameCapBytes)
	if len(got) != nameCapBytes {
		t.Fatalf("expected length %d, got %d", nameCapBytes, len(got))
	}
}

// TestSymbolNameCapsAreReasonable guards against future changes that would
// relax the trust-boundary caps below the values Bug 5 mandates.
func TestSymbolNameCapsAreReasonable(t *testing.T) {
	if symbolCapBytes > 32 {
		t.Fatalf("symbol cap (%d) must not exceed 32", symbolCapBytes)
	}
	if nameCapBytes > 128 {
		t.Fatalf("name cap (%d) must not exceed 128", nameCapBytes)
	}
}
