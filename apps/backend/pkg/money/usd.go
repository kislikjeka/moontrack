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
// with exactly 2 decimal places. E.g., 4115226300 → "41.15", 0 → "0.00".
//
// It renders nil as "0.00", which makes an unknown price indistinguishable from
// a known zero one (#79) — downstream that reads as "cost basis 0", so PnL
// counts the whole disposal as profit. Use it ONLY where the value cannot be
// nil. Where it can, use FormatUSDPtr and give the field a *string type so the
// absence survives into the JSON as null. The signature is kept as-is
// deliberately: several callers are out of scope for #79, and widening this
// return type would substitute "" for nil there instead — a second silent
// substitution in place of the first.
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

// FormatUSDPtr formats a USD amount for a JSON field that may be unknown,
// preserving nil.
//
// nil means "this amount is not known" and must reach the client as JSON null
// rather than "0.00" (#79): a pending lot has no cost basis yet, and rendering
// zero tells the client it acquired the asset for free. A known zero still
// formats as "0.00" — only absence maps to nil. Same reasoning as CopyRate and
// CalcUSDValue (#74), applied at the serialization boundary.
//
// Fields fed by this must be declared *string WITHOUT omitempty, so an unknown
// amount ships as an explicit null instead of vanishing from the payload.
func FormatUSDPtr(value *big.Int) *string {
	if value == nil {
		return nil
	}
	formatted := FormatUSD(value)
	return &formatted
}
