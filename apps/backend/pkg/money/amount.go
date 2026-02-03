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
func FromBaseUnits(amount *big.Int, decimals int) string {
	if amount == nil {
		return "0"
	}

	str := amount.String()
	if decimals == 0 {
		return str
	}

	// Pad with leading zeros if necessary
	for len(str) <= decimals {
		str = "0" + str
	}

	// Insert decimal point
	pos := len(str) - decimals
	result := str[:pos] + "." + str[pos:]

	// Trim trailing zeros after decimal point
	result = strings.TrimRight(result, "0")
	result = strings.TrimRight(result, ".")

	if result == "" {
		return "0"
	}

	return result
}
