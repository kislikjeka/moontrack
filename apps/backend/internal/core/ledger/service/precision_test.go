package service_test

import (
	"math/big"
	"testing"

	"github.com/google/uuid"
	"github.com/kislikjeka/moontrack/internal/core/ledger/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T037: Test big.Int precision with max values
// Per constitution Principle IV: All financial amounts must use *big.Int
func TestBigIntPrecision(t *testing.T) {
	t.Run("max uint256 value - 2^256-1", func(t *testing.T) {
		// Max uint256: 2^256 - 1
		// This is the maximum value that can be stored in Ethereum/blockchain uint256
		maxUint256 := new(big.Int)
		maxUint256.Exp(big.NewInt(2), big.NewInt(256), nil) // 2^256
		maxUint256.Sub(maxUint256, big.NewInt(1))           // 2^256 - 1

		// Expected value: 115792089237316195423570985008687907853269984665640564039457584007913129639935
		expectedStr := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
		assert.Equal(t, expectedStr, maxUint256.String())

		// Create entry with max value
		entry := &domain.Entry{
			ID:          uuid.New(),
			DebitCredit: domain.Debit,
			Amount:      maxUint256,
			AssetID:     "TEST",
			USDRate:     big.NewInt(100000000), // $1 with 10^8 scaling
			USDValue:    new(big.Int).Div(new(big.Int).Mul(maxUint256, big.NewInt(100000000)), big.NewInt(100000000)),
		}

		// Validate entry
		err := entry.Validate()
		require.NoError(t, err)

		// Ensure no precision loss
		assert.Equal(t, expectedStr, entry.Amount.String())
	})

	t.Run("large BTC amount - 21 million BTC in satoshis", func(t *testing.T) {
		// Maximum BTC supply: 21,000,000 BTC
		// In satoshis: 21,000,000 * 100,000,000 = 2,100,000,000,000,000
		maxBTC := big.NewInt(21000000)
		satoshiPerBTC := big.NewInt(100000000)
		maxSatoshi := new(big.Int).Mul(maxBTC, satoshiPerBTC)

		expectedSatoshi := "2100000000000000"
		assert.Equal(t, expectedSatoshi, maxSatoshi.String())

		// No precision loss with big.Int
		entry := &domain.Entry{
			ID:          uuid.New(),
			DebitCredit: domain.Debit,
			Amount:      maxSatoshi,
			AssetID:     "BTC",
			USDRate:     big.NewInt(4500000000000), // $45,000 per BTC with 10^8 scaling
			USDValue:    new(big.Int).Div(new(big.Int).Mul(maxSatoshi, big.NewInt(4500000000000)), big.NewInt(100000000)),
		}

		err := entry.Validate()
		require.NoError(t, err)
		assert.Equal(t, expectedSatoshi, entry.Amount.String())
	})

	t.Run("large ETH amount - 1 billion ETH in wei", func(t *testing.T) {
		// 1 billion ETH in wei: 1,000,000,000 * 10^18
		oneBillionETH := big.NewInt(1000000000)
		weiPerETH := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
		totalWei := new(big.Int).Mul(oneBillionETH, weiPerETH)

		expectedWei := "1000000000000000000000000000"
		assert.Equal(t, expectedWei, totalWei.String())

		entry := &domain.Entry{
			ID:          uuid.New(),
			DebitCredit: domain.Debit,
			Amount:      totalWei,
			AssetID:     "ETH",
			USDRate:     big.NewInt(250000000000), // $2,500 per ETH with 10^8 scaling
			USDValue:    new(big.Int).Div(new(big.Int).Mul(totalWei, big.NewInt(250000000000)), weiPerETH),
		}

		err := entry.Validate()
		require.NoError(t, err)
		assert.Equal(t, expectedWei, entry.Amount.String())
	})

	t.Run("arithmetic operations preserve precision", func(t *testing.T) {
		// Test addition
		a := big.NewInt(123456789012345678)
		b := big.NewInt(987654321098765432)
		sum := new(big.Int).Add(a, b)
		expectedSum := "1111111110111111110"
		assert.Equal(t, expectedSum, sum.String())

		// Test subtraction
		diff := new(big.Int).Sub(b, a)
		expectedDiff := "864197532086419754"
		assert.Equal(t, expectedDiff, diff.String())

		// Test multiplication
		product := new(big.Int).Mul(big.NewInt(123456789), big.NewInt(987654321))
		expectedProduct := "121932631112635269"
		assert.Equal(t, expectedProduct, product.String())

		// Test division (no remainder)
		dividend := big.NewInt(1000000000000)
		divisor := big.NewInt(100000000)
		quotient := new(big.Int).Div(dividend, divisor)
		expectedQuotient := "10000"
		assert.Equal(t, expectedQuotient, quotient.String())
	})

	t.Run("USD rate calculation with scaling", func(t *testing.T) {
		// Amount: 1.5 BTC = 150,000,000 satoshis
		amount := big.NewInt(150000000)

		// USD Rate: $45,678.90 per BTC, scaled by 10^8 = 4567890000000
		usdRate := big.NewInt(4567890000000)

		// USD Value = (amount * usdRate) / 10^8
		usdValue := new(big.Int).Mul(amount, usdRate)
		usdValue.Div(usdValue, big.NewInt(100000000))

		// Expected: 1.5 * $45,678.90 = $68,518.35, scaled by 10^8 = 6851835000000
		expectedUSDValue := "6851835000000"
		assert.Equal(t, expectedUSDValue, usdValue.String())

		// Verify no precision loss in reverse calculation
		reversedAmount := new(big.Int).Mul(usdValue, big.NewInt(100000000))
		reversedAmount.Div(reversedAmount, usdRate)
		assert.Equal(t, amount.String(), reversedAmount.String())
	})

	t.Run("transaction balance with large numbers", func(t *testing.T) {
		// Create a balanced transaction with very large amounts
		largeAmount := new(big.Int)
		largeAmount.Exp(big.NewInt(10), big.NewInt(30), nil) // 10^30

		debitSum := new(big.Int).Set(largeAmount)
		creditSum := new(big.Int).Set(largeAmount)

		// Verify balance
		assert.Equal(t, 0, debitSum.Cmp(creditSum), "Large amounts must balance exactly")

		// Add small amount to one side
		debitSum.Add(debitSum, big.NewInt(1))

		// Now they should not balance
		assert.NotEqual(t, 0, debitSum.Cmp(creditSum), "Even 1 unit difference should be detected")
	})

	t.Run("prevent float64 precision errors", func(t *testing.T) {
		// This test demonstrates why float64 is FORBIDDEN for financial amounts

		// Using float64 (WRONG - DO NOT USE IN PRODUCTION)
		var f1 float64 = 0.1
		var f2 float64 = 0.2
		floatSum := f1 + f2
		// Float precision error: 0.1 + 0.2 != 0.3 in float64
		assert.NotEqual(t, 0.3, floatSum) // This would fail with float64!

		// Using big.Int (CORRECT - Constitution mandated)
		// Represent 0.1 and 0.2 with 10^8 scaling
		b1 := big.NewInt(10000000) // 0.1 * 10^8
		b2 := big.NewInt(20000000) // 0.2 * 10^8
		bigSum := new(big.Int).Add(b1, b2)
		expected := big.NewInt(30000000) // 0.3 * 10^8

		// With big.Int, this works perfectly
		assert.Equal(t, 0, bigSum.Cmp(expected), "big.Int has exact precision")
		assert.Equal(t, "30000000", bigSum.String())
	})
}

// TestBigIntSerialization tests that big.Int can be serialized/deserialized
func TestBigIntSerialization(t *testing.T) {
	t.Run("string representation maintains precision", func(t *testing.T) {
		original := new(big.Int)
		original.Exp(big.NewInt(2), big.NewInt(200), nil) // 2^200

		// Convert to string
		str := original.String()

		// Parse back from string
		parsed := new(big.Int)
		parsed.SetString(str, 10)

		// Verify they're equal
		assert.Equal(t, 0, original.Cmp(parsed))
		assert.Equal(t, original.String(), parsed.String())
	})

	t.Run("bytes representation maintains precision", func(t *testing.T) {
		original := new(big.Int)
		original.Exp(big.NewInt(2), big.NewInt(256), nil)
		original.Sub(original, big.NewInt(1)) // Max uint256

		// Convert to bytes
		bytes := original.Bytes()

		// Parse back from bytes
		parsed := new(big.Int).SetBytes(bytes)

		// Verify they're equal
		assert.Equal(t, 0, original.Cmp(parsed))
		assert.Equal(t, original.String(), parsed.String())
	})
}
