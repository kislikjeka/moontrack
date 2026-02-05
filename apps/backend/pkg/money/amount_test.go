package money

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToBaseUnits_WholeNumber(t *testing.T) {
	result, err := ToBaseUnits("1", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(100000000), result)
}

func TestToBaseUnits_WithDecimals(t *testing.T) {
	result, err := ToBaseUnits("1.5", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(150000000), result)
}

func TestToBaseUnits_SmallAmount(t *testing.T) {
	// Minimum satoshi
	result, err := ToBaseUnits("0.00000001", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(1), result)
}

func TestToBaseUnits_VerySmallAmount(t *testing.T) {
	result, err := ToBaseUnits("0.0005", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(50000), result)
}

func TestToBaseUnits_LargeNumber(t *testing.T) {
	// Max BTC supply
	result, err := ToBaseUnits("21000000", 8)
	require.NoError(t, err)
	expected := new(big.Int)
	expected.SetString("2100000000000000", 10)
	assert.Equal(t, expected, result)
}

func TestToBaseUnits_EthereumWei(t *testing.T) {
	result, err := ToBaseUnits("1.5", 18)
	require.NoError(t, err)
	expected := new(big.Int)
	expected.SetString("1500000000000000000", 10)
	assert.Equal(t, expected, result)
}

func TestToBaseUnits_ZeroDecimals(t *testing.T) {
	result, err := ToBaseUnits("100", 0)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(100), result)
}

func TestToBaseUnits_LeadingZeros(t *testing.T) {
	result, err := ToBaseUnits("0.001", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(100000), result)
}

func TestToBaseUnits_EmptyString(t *testing.T) {
	_, err := ToBaseUnits("", 8)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "amount is required")
}

func TestToBaseUnits_InvalidFormat(t *testing.T) {
	_, err := ToBaseUnits("abc", 8)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid amount format")
}

func TestToBaseUnits_NegativeNumber(t *testing.T) {
	result, err := ToBaseUnits("-1", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(-100000000), result)
}

func TestToBaseUnits_TruncatesExtraDecimals(t *testing.T) {
	// 1.123456789 with 8 decimals should truncate to 1.12345678
	result, err := ToBaseUnits("1.123456789", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(112345678), result)
}

func TestToBaseUnits_NoIntegerPart(t *testing.T) {
	result, err := ToBaseUnits(".5", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(50000000), result)
}

func TestToBaseUnits_Zero(t *testing.T) {
	result, err := ToBaseUnits("0", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(0), result)
}

func TestToBaseUnits_ZeroWithDecimals(t *testing.T) {
	result, err := ToBaseUnits("0.00", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(0), result)
}

func TestToBaseUnits_MoreDecimalsThanScale(t *testing.T) {
	// 0.123456789012345678 with 8 decimals should truncate
	result, err := ToBaseUnits("0.123456789012345678", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(12345678), result)
}

func TestToBaseUnits_FewerDecimalsThanScale(t *testing.T) {
	// 1.1 with 8 decimals should pad
	result, err := ToBaseUnits("1.1", 8)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(110000000), result)
}

func TestToBaseUnits_VeryLargeWithPrecision(t *testing.T) {
	// Test with 78-digit precision (matches NUMERIC(78,0))
	result, err := ToBaseUnits("123456789012345678901234567890", 0)
	require.NoError(t, err)
	expected := new(big.Int)
	expected.SetString("123456789012345678901234567890", 10)
	assert.Equal(t, expected, result)
}

// FromBaseUnits tests

func TestFromBaseUnits_BasicConversion(t *testing.T) {
	result := FromBaseUnits(big.NewInt(150000000), 8)
	assert.Equal(t, "1.5", result)
}

func TestFromBaseUnits_WholeNumber(t *testing.T) {
	result := FromBaseUnits(big.NewInt(100000000), 8)
	assert.Equal(t, "1", result) // No trailing zeros
}

func TestFromBaseUnits_SmallAmount(t *testing.T) {
	result := FromBaseUnits(big.NewInt(1), 8)
	assert.Equal(t, "0.00000001", result)
}

func TestFromBaseUnits_Zero(t *testing.T) {
	result := FromBaseUnits(big.NewInt(0), 8)
	assert.Equal(t, "0", result)
}

func TestFromBaseUnits_NilInput(t *testing.T) {
	result := FromBaseUnits(nil, 8)
	assert.Equal(t, "0", result)
}

func TestFromBaseUnits_ZeroDecimals(t *testing.T) {
	result := FromBaseUnits(big.NewInt(100), 0)
	assert.Equal(t, "100", result)
}

func TestFromBaseUnits_LargeNumber(t *testing.T) {
	amount := new(big.Int)
	amount.SetString("2100000000000000", 10)
	result := FromBaseUnits(amount, 8)
	assert.Equal(t, "21000000", result)
}

func TestFromBaseUnits_Ethereum(t *testing.T) {
	amount := new(big.Int)
	amount.SetString("1500000000000000000", 10)
	result := FromBaseUnits(amount, 18)
	assert.Equal(t, "1.5", result)
}

func TestFromBaseUnits_TrimsTrailingZeros(t *testing.T) {
	result := FromBaseUnits(big.NewInt(150000000), 8)
	assert.Equal(t, "1.5", result) // Not "1.50000000"
}

func TestFromBaseUnits_SingleDigitFraction(t *testing.T) {
	result := FromBaseUnits(big.NewInt(10000000), 8)
	assert.Equal(t, "0.1", result)
}

func TestFromBaseUnits_NegativeAmount(t *testing.T) {
	result := FromBaseUnits(big.NewInt(-150000000), 8)
	assert.Equal(t, "-1.5", result)
}

// Round-trip tests

func TestRoundTrip_BTC(t *testing.T) {
	original := "1.5"
	baseUnits, err := ToBaseUnits(original, 8)
	require.NoError(t, err)
	back := FromBaseUnits(baseUnits, 8)
	assert.Equal(t, original, back)
}

func TestRoundTrip_ETH(t *testing.T) {
	original := "0.123456789012345678"
	baseUnits, err := ToBaseUnits(original, 18)
	require.NoError(t, err)
	back := FromBaseUnits(baseUnits, 18)
	assert.Equal(t, original, back)
}

func TestRoundTrip_WholeNumber(t *testing.T) {
	original := "21000000"
	baseUnits, err := ToBaseUnits(original, 8)
	require.NoError(t, err)
	back := FromBaseUnits(baseUnits, 8)
	assert.Equal(t, original, back)
}

func TestRoundTrip_SmallAmount(t *testing.T) {
	original := "0.00000001"
	baseUnits, err := ToBaseUnits(original, 8)
	require.NoError(t, err)
	back := FromBaseUnits(baseUnits, 8)
	assert.Equal(t, original, back)
}

func TestRoundTrip_Zero(t *testing.T) {
	original := "0"
	baseUnits, err := ToBaseUnits(original, 8)
	require.NoError(t, err)
	back := FromBaseUnits(baseUnits, 8)
	assert.Equal(t, original, back)
}

// Edge case: very small amounts that might be affected by precision
func TestToBaseUnits_USDCPrecision(t *testing.T) {
	// USDC has 6 decimals
	result, err := ToBaseUnits("1.234567", 6)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(1234567), result)
}

func TestFromBaseUnits_USDCPrecision(t *testing.T) {
	// USDC has 6 decimals
	result := FromBaseUnits(big.NewInt(1234567), 6)
	assert.Equal(t, "1.234567", result)
}
