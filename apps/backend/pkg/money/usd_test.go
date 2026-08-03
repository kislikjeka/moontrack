package money

import (
	"math/big"
	"testing"
)

// TestFormatUSD_NilRendersAsZero pins the legacy behaviour rather than endorsing
// it: FormatUSD returns a string, so it has no way to say "unknown" and answers
// nil with "0.00". That substitution is the #79 defect. The fix is to call
// FormatUSDPtr on any field that can be nil, NOT to change this function — the
// test exists so a future change to it is a deliberate one.
func TestFormatUSD_NilRendersAsZero(t *testing.T) {
	if got := FormatUSD(nil); got != "0.00" {
		t.Errorf("FormatUSD(nil) = %q, want %q", got, "0.00")
	}
}

func TestFormatUSD_Values(t *testing.T) {
	cases := []struct {
		name  string
		value *big.Int
		want  string
	}{
		{"zero", big.NewInt(0), "0.00"},
		{"whole dollar", big.NewInt(100000000), "1.00"},
		{"truncates below cents", big.NewInt(4115226300), "41.15"},
		{"sub-cent truncates to zero", big.NewInt(1), "0.00"},
		{"negative", big.NewInt(-4115226300), "-41.15"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatUSD(tc.value); got != tc.want {
				t.Errorf("FormatUSD(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestFormatUSDPtr_NilStaysNil is the contract #79 turns on: an unknown USD
// amount must reach the client as JSON null, never as "0.00". A zero-valued
// amount is a KNOWN zero and must still render.
func TestFormatUSDPtr_NilStaysNil(t *testing.T) {
	if got := FormatUSDPtr(nil); got != nil {
		t.Errorf("FormatUSDPtr(nil) = %v, want nil", *got)
	}
}

func TestFormatUSDPtr_KnownZeroIsNotNil(t *testing.T) {
	got := FormatUSDPtr(big.NewInt(0))
	if got == nil {
		t.Fatal("FormatUSDPtr(0) = nil, want a pointer to \"0.00\" — a known zero is not an absence")
	}
	if *got != "0.00" {
		t.Errorf("FormatUSDPtr(0) = %q, want %q", *got, "0.00")
	}
}

func TestFormatUSDPtr_MatchesFormatUSD(t *testing.T) {
	values := []*big.Int{
		big.NewInt(0),
		big.NewInt(100000000),
		big.NewInt(4115226300),
		big.NewInt(-4115226300),
	}

	for _, v := range values {
		got := FormatUSDPtr(v)
		if got == nil {
			t.Fatalf("FormatUSDPtr(%v) = nil, want non-nil", v)
		}
		if want := FormatUSD(v); *got != want {
			t.Errorf("FormatUSDPtr(%v) = %q, want %q (must agree with FormatUSD)", v, *got, want)
		}
	}
}
