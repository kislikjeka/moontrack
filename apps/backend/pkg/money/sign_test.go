package money

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bigFromString is a test helper for values that exceed int64.
func bigFromString(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	require.True(t, ok, "bad test fixture: %q is not a base-10 integer", s)
	return v
}

// TestFromBaseUnits_NegativeSignPlacement is the #71 regression.
//
// The old implementation inserted the decimal point by offset into
// amount.String(), which carries the '-'. The sign therefore counted as a digit
// during zero-padding, so whenever |value| was small enough that the integer
// part was zero (or a single digit), the point landed inside the digit run and
// produced strings like "0.0000-1" and "-.999999" that parse as nothing at all.
func TestFromBaseUnits_NegativeSignPlacement(t *testing.T) {
	cases := []struct {
		name     string
		value    *big.Int
		decimals int
		want     string
	}{
		// The exact value from the issue report.
		{"issue #71 report", bigFromString(t, "-662337582090000"), 18, "-0.00066233758209"},

		// |value| < 10^decimals: integer part is zero, sign has nowhere to go
		// but in front. This is the shape that produced "0.0000-1".
		{"smallest negative unit, 6 decimals", big.NewInt(-1), 6, "-0.000001"},
		{"smallest negative unit, 18 decimals", bigFromString(t, "-1"), 18, "-0.000000000000000001"},
		{"smallest negative unit, 8 decimals", big.NewInt(-1), 8, "-0.00000001"},

		// |value| == 10^decimals - 1: the padding loop stops one short, which is
		// how the old code emitted a leading "-." with no integer digit.
		{"one below unit, 6 decimals", big.NewInt(-999999), 6, "-0.999999"},
		{"one below unit, 1 decimal", big.NewInt(-5), 1, "-0.5"},
		{"one below unit, 8 decimals", big.NewInt(-99999999), 8, "-0.99999999"},

		// |value| >= 10^decimals: these already worked; they must stay working.
		{"exactly one unit", bigFromString(t, "-1000000000000000000"), 18, "-1"},
		{"whole and fraction", big.NewInt(-123456789), 8, "-1.23456789"},
		{"trims trailing zeros", big.NewInt(-1500000), 6, "-1.5"},
		{"zero decimals", big.NewInt(-100), 0, "-100"},
		{"large negative", bigFromString(t, "-2100000000000000"), 8, "-21000000"},

		// Zero must never carry a sign, however it was constructed.
		{"zero", big.NewInt(0), 6, "0"},
		{"negated zero", new(big.Int).Neg(big.NewInt(0)), 6, "0"},
		{"zero, 18 decimals", big.NewInt(0), 18, "0"},

		// Positive regression guards.
		{"positive smallest unit", big.NewInt(1), 6, "0.000001"},
		{"positive one below unit", big.NewInt(999999), 6, "0.999999"},
		{"positive whole", big.NewInt(1500000), 6, "1.5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FromBaseUnits(tc.value, tc.decimals)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestFromBaseUnits_OutputIsAlwaysParseable is the property the #71 strings
// violated: whatever FromBaseUnits prints must be readable back as a number.
// It catches malformed output shapes that an equality table would miss only
// because nobody thought to enumerate them.
func TestFromBaseUnits_OutputIsAlwaysParseable(t *testing.T) {
	values := []string{
		"-1", "-9", "-10", "-99", "-999999", "-1000000", "-662337582090000",
		"-1000000000000000000", "0", "1", "999999", "123456789012345678901234567890",
		"-123456789012345678901234567890",
	}
	decimalScales := []int{0, 1, 6, 8, 18}

	for _, v := range values {
		for _, d := range decimalScales {
			amount := bigFromString(t, v)
			got := FromBaseUnits(amount, d)

			parsed, ok := new(big.Rat).SetString(got)
			require.True(t, ok, "FromBaseUnits(%s, %d) = %q, which does not parse as a number", v, d, got)

			// Sign must agree with the input, and only zero may be unsigned.
			assert.Equal(t, amount.Sign(), parsed.Sign(),
				"FromBaseUnits(%s, %d) = %q has the wrong sign", v, d, got)
		}
	}
}

// TestRoundTrip_Negative closes the loop demanded by the #71 acceptance
// criteria: a negative value must survive ToBaseUnits(FromBaseUnits(v)).
func TestRoundTrip_Negative(t *testing.T) {
	values := []string{
		"-1",
		"-999999",
		"-1500000",
		"-662337582090000",
		"-1000000000000000000",
		"-123456789",
	}
	decimalScales := []int{0, 6, 8, 18}

	for _, v := range values {
		for _, d := range decimalScales {
			original := bigFromString(t, v)

			rendered := FromBaseUnits(original, d)
			back, err := ToBaseUnits(rendered, d)
			require.NoError(t, err, "ToBaseUnits(%q, %d) failed", rendered, d)

			assert.Equal(t, 0, original.Cmp(back),
				"round-trip lost value: %s -> %q -> %s (decimals=%d)", original, rendered, back, d)
		}
	}
}

// TestToBaseUnits_NegativeParsing guards the parser against the mirror image of
// the #71 bug. It already handles these correctly — "-0" trims to zero without
// a stray sign, and a sub-unit negative keeps its sign through the
// leading-zero trim — so this is a pin, not a fix.
func TestToBaseUnits_NegativeParsing(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		decimals int
		want     string
	}{
		{"sub-unit negative", "-0.000001", 6, "-1"},
		{"negative below one", "-0.00066233758209", 18, "-662337582090000"},
		{"negative one below unit", "-0.999999", 6, "-999999"},
		{"negative whole", "-1", 6, "-1000000"},
		{"negative with fraction", "-1.5", 6, "-1500000"},
		{"negative zero is zero", "-0", 6, "0"},
		{"negative zero with decimals", "-0.0", 6, "0"},
		{"negative truncates toward zero", "-1.9", 0, "-1"},
		{"positive truncates toward zero", "1.9", 0, "1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToBaseUnits(tc.input, tc.decimals)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())
			// A zero result must never be a "-0" that stringifies with a sign.
			if got.Sign() == 0 {
				assert.Equal(t, "0", got.String())
			}
		})
	}
}

// TestFormatUSD_NegativeSignPlacement checks usd.go for the same class of
// defect. FormatUSD strips the sign before padding, so it is already correct;
// these cases pin that, including the sub-cent values where truncation toward
// zero yields "-0.00" — a real magnitude smaller than a cent, not a lost sign.
func TestFormatUSD_NegativeSignPlacement(t *testing.T) {
	cases := []struct {
		name  string
		value *big.Int
		want  string
	}{
		{"negative whole dollar", big.NewInt(-100000000), "-1.00"},
		{"negative with cents", big.NewInt(-4115226300), "-41.15"},
		{"negative below a dollar", big.NewInt(-12345678), "-0.12"},
		{"negative sub-cent truncates to zero magnitude", big.NewInt(-1), "-0.00"},
		{"negative exactly one cent", big.NewInt(-1000000), "-0.01"},
		{"zero is unsigned", big.NewInt(0), "0.00"},
		{"negated zero is unsigned", new(big.Int).Neg(big.NewInt(0)), "0.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatUSD(tc.value)
			assert.Equal(t, tc.want, got)

			ptr := FormatUSDPtr(tc.value)
			require.NotNil(t, ptr)
			assert.Equal(t, tc.want, *ptr, "FormatUSDPtr must agree with FormatUSD")
		})
	}
}
