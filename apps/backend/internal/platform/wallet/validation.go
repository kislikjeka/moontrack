package wallet

import (
	"encoding/hex"
	"regexp"
	"strings"

	"golang.org/x/crypto/sha3"
)

// EVM address regex: 0x followed by exactly 40 hex characters
var evmAddressRegex = regexp.MustCompile(`^0x[a-fA-F0-9]{40}$`)

// ValidateEVMAddress validates an EVM address and returns the EIP-55 checksummed version.
// Returns an error if the address is invalid.
func ValidateEVMAddress(address string) (string, error) {
	if address == "" {
		return "", ErrMissingAddress
	}

	// Check basic format
	if !evmAddressRegex.MatchString(address) {
		return "", ErrInvalidAddress
	}

	// Convert to checksummed address (EIP-55)
	checksummed := ToChecksumAddress(address)

	// If the input was checksummed, verify it matches
	if isChecksummed(address) && address != checksummed {
		return "", ErrInvalidChecksum
	}

	return checksummed, nil
}

// ToChecksumAddress converts an EVM address to EIP-55 checksummed format.
// https://eips.ethereum.org/EIPS/eip-55
func ToChecksumAddress(address string) string {
	// Remove 0x prefix and lowercase
	addr := strings.ToLower(strings.TrimPrefix(address, "0x"))

	// Keccak-256 hash of the lowercase address
	hash := keccak256([]byte(addr))

	// Build checksummed address
	var result strings.Builder
	result.WriteString("0x")

	for i, c := range addr {
		if c >= '0' && c <= '9' {
			result.WriteRune(c)
		} else {
			// Check if the corresponding hex nibble in hash is >= 8
			hashByte := hash[i/2]
			var nibble byte
			if i%2 == 0 {
				nibble = hashByte >> 4
			} else {
				nibble = hashByte & 0x0F
			}

			if nibble >= 8 {
				result.WriteRune(c - 32) // Uppercase
			} else {
				result.WriteRune(c) // Lowercase
			}
		}
	}

	return result.String()
}

// isChecksummed checks if an address appears to be checksummed (has mixed case)
func isChecksummed(address string) bool {
	addr := strings.TrimPrefix(address, "0x")
	hasUpper := false
	hasLower := false

	for _, c := range addr {
		if c >= 'A' && c <= 'F' {
			hasUpper = true
		}
		if c >= 'a' && c <= 'f' {
			hasLower = true
		}
	}

	return hasUpper && hasLower
}

// keccak256 computes the Keccak-256 hash
func keccak256(data []byte) []byte {
	hash := sha3.NewLegacyKeccak256()
	hash.Write(data)
	return hash.Sum(nil)
}

// NormalizeAddress normalizes an EVM address to lowercase with 0x prefix.
// Useful for comparisons and database lookups.
func NormalizeAddress(address string) string {
	return strings.ToLower(strings.TrimPrefix(address, "0x"))
}

// AddressesEqual compares two EVM addresses case-insensitively
func AddressesEqual(a, b string) bool {
	return strings.EqualFold(
		strings.TrimPrefix(a, "0x"),
		strings.TrimPrefix(b, "0x"),
	)
}

// IsZeroAddress checks if an address is the zero address
func IsZeroAddress(address string) bool {
	normalized := NormalizeAddress(address)
	return normalized == "0000000000000000000000000000000000000000"
}

// HexToBytes converts a hex string (with or without 0x prefix) to bytes
func HexToBytes(hexStr string) ([]byte, error) {
	hexStr = strings.TrimPrefix(hexStr, "0x")
	return hex.DecodeString(hexStr)
}
