package money

import (
	"fmt"
	"math/big"
	"strings"
)

// ToBaseUnits converts a human-readable amount string to base units (big.Int)
// Handles decimal inputs like "0.0005" → 50000 (for BTC with 8 decimals)
// "1.5" BTC → 150000000
func ToBaseUnits(amountStr string, decimals int) (*big.Int, error) {
	if amountStr == "" {
		return nil, fmt.Errorf("amount is required")
	}

	// Use string manipulation to avoid floating point precision issues
	// Split into integer and decimal parts
	parts := strings.Split(amountStr, ".")

	intPart := parts[0]
	if intPart == "" {
		intPart = "0"
	}

	decPart := ""
	if len(parts) > 1 {
		decPart = parts[1]
	}

	// Pad or truncate decimal part to match decimals
	if len(decPart) < decimals {
		decPart = decPart + strings.Repeat("0", decimals-len(decPart))
	} else if len(decPart) > decimals {
		decPart = decPart[:decimals]
	}

	// Combine integer and decimal parts
	combined := intPart + decPart

	// Remove leading zeros (but keep at least one digit)
	combined = strings.TrimLeft(combined, "0")
	if combined == "" {
		combined = "0"
	}

	// Parse as big.Int
	result := new(big.Int)
	if _, ok := result.SetString(combined, 10); !ok {
		return nil, fmt.Errorf("invalid amount format")
	}

	return result, nil
}

// FromBaseUnits converts base units (big.Int) to a human-readable string
// E.g., 150000000 with 8 decimals → "1.5"
//
// The sign is split off before any digit arithmetic and re-attached at the end.
// Formatting works by offset into the digit string, and '-' is not a digit: when
// it was left in place the padding loop counted it, so any value whose integer
// part was zero had the decimal point inserted inside its digits, yielding
// unparseable output like "0.0000-1" or "-.999999" (#71).
func FromBaseUnits(amount *big.Int, decimals int) string {
	if amount == nil {
		return "0"
	}

	// Sign.Abs rather than string surgery: magnitude and sign are separate facts,
	// and only the magnitude takes part in positioning the decimal point. Zero
	// has Sign() == 0, so a big.Int built via Neg(0) never grows a "-0" here.
	negative := amount.Sign() < 0
	str := new(big.Int).Abs(amount).String()

	if decimals > 0 {
		// Pad so there is at least one digit left of the decimal point.
		for len(str) <= decimals {
			str = "0" + str
		}

		// Insert decimal point
		pos := len(str) - decimals
		str = str[:pos] + "." + str[pos:]

		// Trim trailing zeros after decimal point
		str = strings.TrimRight(str, "0")
		str = strings.TrimRight(str, ".")
	}

	if str == "" {
		return "0"
	}

	// A magnitude that trims away to nothing is zero, which carries no sign.
	if negative && str != "0" {
		str = "-" + str
	}

	return str
}
