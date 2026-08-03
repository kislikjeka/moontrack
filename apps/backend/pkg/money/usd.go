package money

import "math/big"

// CopyRate returns an independent copy of a USD rate, preserving nil.
//
// nil means "the price at this moment is not known" and must survive the copy:
// entry builders used to write new(big.Int).Set(rate), which panics on nil and
// so forced every caller to substitute zero first. That substitution is what
// froze cost basis at 0 with price_status='resolved' (#74). Use this wherever a
// rate is copied into an Entry.
func CopyRate(rate *big.Int) *big.Int {
	if rate == nil {
		return nil
	}
	return new(big.Int).Set(rate)
}

// CalcUSDValue computes (amount * usdRate) / 10^decimals — result is USD value
// scaled by 10^8. Returns nil when usdRate is nil: an unknown rate yields an
// unknown value, not a zero one (#74).
func CalcUSDValue(amount, usdRate *big.Int, decimals int) *big.Int {
	if usdRate == nil {
		return nil
	}
	if usdRate.Sign() == 0 {
		return big.NewInt(0)
	}
	value := new(big.Int).Mul(amount, usdRate)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	value.Div(value, divisor)
	return value
}

// FormatUSD converts a big.Int scaled by 10^8 to a human-readable decimal string
// with exactly 2 decimal places. E.g., 4115226300 → "41.15", 0 → "0.00", nil → "0.00"
func FormatUSD(value *big.Int) string {
	if value == nil {
		return "0.00"
	}

	const usdDecimals = 8

	str := value.String()

	// Handle negative values
	negative := false
	if len(str) > 0 && str[0] == '-' {
		negative = true
		str = str[1:]
	}

	// Pad with leading zeros if necessary
	for len(str) <= usdDecimals {
		str = "0" + str
	}

	// Insert decimal point at position len-8
	pos := len(str) - usdDecimals
	intPart := str[:pos]
	fracPart := str[pos:]

	// Keep exactly 2 decimal places (truncate, matching financial convention)
	if len(fracPart) > 2 {
		fracPart = fracPart[:2]
	} else {
		for len(fracPart) < 2 {
			fracPart = fracPart + "0"
		}
	}

	result := intPart + "." + fracPart
	if negative {
		result = "-" + result
	}

	return result
}
