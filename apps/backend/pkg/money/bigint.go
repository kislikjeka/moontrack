package money

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// BigInt is a wrapper around big.Int that supports JSON unmarshaling from string
type BigInt struct {
	*big.Int
}

// NewBigInt creates a new BigInt from a big.Int
func NewBigInt(i *big.Int) *BigInt {
	if i == nil {
		return nil
	}
	return &BigInt{Int: i}
}

// NewBigIntFromInt64 creates a new BigInt from an int64
func NewBigIntFromInt64(i int64) *BigInt {
	return &BigInt{Int: big.NewInt(i)}
}

// NewBigIntFromString creates a new BigInt from a string
func NewBigIntFromString(s string) (*BigInt, bool) {
	i := new(big.Int)
	if _, ok := i.SetString(s, 10); !ok {
		return nil, false
	}
	return &BigInt{Int: i}, true
}

// UnmarshalJSON implements json.Unmarshaler
// Supports: "123", 123, null
func (b *BigInt) UnmarshalJSON(data []byte) error {
	// Handle null
	if string(data) == "null" {
		b.Int = nil
		return nil
	}

	// Try to unmarshal as string first (most common case from our API)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		i := new(big.Int)
		if _, ok := i.SetString(s, 10); !ok {
			return fmt.Errorf("invalid BigInt string: %s", s)
		}
		b.Int = i
		return nil
	}

	// Try to unmarshal as number
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		i := new(big.Int)
		if _, ok := i.SetString(n.String(), 10); !ok {
			return fmt.Errorf("invalid BigInt number: %s", n.String())
		}
		b.Int = i
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into BigInt", string(data))
}

// MarshalJSON implements json.Marshaler
func (b *BigInt) MarshalJSON() ([]byte, error) {
	if b == nil || b.Int == nil {
		return []byte("null"), nil
	}
	return json.Marshal(b.Int.String())
}

// ToBigInt returns the underlying *big.Int
func (b *BigInt) ToBigInt() *big.Int {
	if b == nil {
		return nil
	}
	return b.Int
}

// IsNil returns true if the BigInt is nil
func (b *BigInt) IsNil() bool {
	return b == nil || b.Int == nil
}

// IsZero returns true if the BigInt is zero
func (b *BigInt) IsZero() bool {
	return b.IsNil() || b.Int.Sign() == 0
}

// IsPositive returns true if the BigInt is positive (> 0)
func (b *BigInt) IsPositive() bool {
	return !b.IsNil() && b.Int.Sign() > 0
}
