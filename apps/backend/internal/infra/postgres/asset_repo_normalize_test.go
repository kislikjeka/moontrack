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
	chains := []string{
		// Historical set.
		"ethereum", "arbitrum", "optimism", "base", "polygon",
		"bnb-chain", "binance-smart-chain", "avalanche", "linea", "zksync", "scroll",
		// Extended EVM chains Zerion also returns assets for.
		"mantle", "blast", "celo", "gnosis", "xdai", "fantom",
		"cronos", "moonbeam", "arbitrum-nova", "sonic", "unichain",
	}
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

// TestNormalizeContractAddress_MantlePassesRegex is an explicit regression
// guard — Bug E added mantle (and other extended chains) to the EVM allow-
// list. Without that, mantle addresses bypassed the shape check and ended
// up in the database un-normalized, opening dedupe gaps.
func TestNormalizeContractAddress_MantlePassesRegex(t *testing.T) {
	addr := "0xabcdef0123456789abcdef0123456789abcdef01"
	got, err := normalizeContractAddress("mantle", addr)
	if err != nil {
		t.Fatalf("mantle address rejected: %v", err)
	}
	if got != addr {
		t.Fatalf("mantle normalization mismatch: got %q, want %q", got, addr)
	}
	// Case folding must still happen on EVM chains so we dedupe correctly.
	got, err = normalizeContractAddress("mantle", "0xABCDEF0123456789ABCDEF0123456789ABCDEF01")
	if err != nil {
		t.Fatalf("uppercase mantle address rejected: %v", err)
	}
	if got != addr {
		t.Fatalf("mantle uppercase must fold: got %q, want %q", got, addr)
	}
	// Invalid shape on mantle must still reject.
	_, err = normalizeContractAddress("mantle", "not-an-addr")
	if !errors.Is(err, asset.ErrInvalidContractAddress) {
		t.Fatalf("mantle: expected ErrInvalidContractAddress, got %v", err)
	}
}

func TestNormalizeContractAddress_NonEVMChains_PreservesCase(t *testing.T) {
	// Solana / Tron / Aptos / Sui addresses are case-sensitive (base58 or
	// mixed-case account IDs). We must TrimSpace only — lowercasing corrupts
	// the address and causes duplicate rows for the "same" token under two
	// different on-chain identities.
	got, err := normalizeContractAddress("solana", "  SomeBase58Addr123  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "SomeBase58Addr123" {
		t.Fatalf("solana address case must be preserved: got %q, want %q",
			got, "SomeBase58Addr123")
	}

	// A realistic Solana mint (USDC on mainnet) — must not be folded.
	usdcMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	got, err = normalizeContractAddress("solana", usdcMint)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != usdcMint {
		t.Fatalf("real Solana mint mis-normalized: got %q, want %q", got, usdcMint)
	}

	// Unknown / cosmos-style chain: also case-preserving.
	got, err = normalizeContractAddress("cosmos-hub", " ABC123 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ABC123" {
		t.Fatalf("got %q, want %q", got, "ABC123")
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

// TestSanitizeProviderField_StripsUTF8LineSeparators verifies UTF-8 line
// separators (U+2028, U+2029), NEL (U+0085), DEL (U+007F), and C1 controls
// are all stripped. These bytes pass through a naive byte-wise control
// filter and can forge log lines in downstream JSON parsers.
func TestSanitizeProviderField_StripsUTF8LineSeparators(t *testing.T) {
	cases := map[string]string{
		"U+2028": "malicious\u2028injected",
		"U+2029": "oops\u2029line2",
		"U+0085": "look\u0085line2",
		"U+007F": "zap\u007fchar",
		"U+0099": "hi\u0099ctrl",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := sanitizeProviderField(in, 500)
			for _, r := range got {
				if r == 0x2028 || r == 0x2029 || r == 0x85 || r == 0x7F || (r >= 0x80 && r < 0xA0) {
					t.Fatalf("%s: forbidden rune U+%04X left in %q", name, r, got)
				}
			}
		})
	}
}

// TestSanitizeProviderField_PreservesSafeUnicode guards against regression:
// non-control Unicode (Cyrillic, CJK, emoji) must not be stripped so that
// legitimate provider-supplied symbol/name values continue to round-trip.
func TestSanitizeProviderField_PreservesSafeUnicode(t *testing.T) {
	in := "привет世界🌍"
	got := sanitizeProviderField(in, 500)
	if got != in {
		t.Fatalf("expected %q preserved, got %q", in, got)
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
