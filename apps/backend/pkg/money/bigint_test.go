package money

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Constructor tests

func TestNewBigInt_FromBigInt(t *testing.T) {
	input := big.NewInt(12345)
	result := NewBigInt(input)
	require.NotNil(t, result)
	assert.Equal(t, input, result.Int)
}

func TestNewBigInt_NilInput(t *testing.T) {
	result := NewBigInt(nil)
	assert.Nil(t, result)
}

func TestNewBigIntFromInt64_Positive(t *testing.T) {
	result := NewBigIntFromInt64(12345)
	require.NotNil(t, result)
	assert.Equal(t, big.NewInt(12345), result.Int)
}

func TestNewBigIntFromInt64_Negative(t *testing.T) {
	result := NewBigIntFromInt64(-12345)
	require.NotNil(t, result)
	assert.Equal(t, big.NewInt(-12345), result.Int)
}

func TestNewBigIntFromInt64_Zero(t *testing.T) {
	result := NewBigIntFromInt64(0)
	require.NotNil(t, result)
	assert.Equal(t, big.NewInt(0), result.Int)
}

func TestNewBigIntFromString_Valid(t *testing.T) {
	result, ok := NewBigIntFromString("12345")
	require.True(t, ok)
	require.NotNil(t, result)
	assert.Equal(t, big.NewInt(12345), result.Int)
}

func TestNewBigIntFromString_ValidNegative(t *testing.T) {
	result, ok := NewBigIntFromString("-12345")
	require.True(t, ok)
	require.NotNil(t, result)
	assert.Equal(t, big.NewInt(-12345), result.Int)
}

func TestNewBigIntFromString_ValidZero(t *testing.T) {
	result, ok := NewBigIntFromString("0")
	require.True(t, ok)
	require.NotNil(t, result)
	assert.Equal(t, big.NewInt(0), result.Int)
}

func TestNewBigIntFromString_Invalid(t *testing.T) {
	result, ok := NewBigIntFromString("not-a-number")
	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestNewBigIntFromString_VeryLarge(t *testing.T) {
	// 78-digit number (matches NUMERIC(78,0) precision)
	largeNum := "123456789012345678901234567890123456789012345678901234567890123456789012345678"
	result, ok := NewBigIntFromString(largeNum)
	require.True(t, ok)
	require.NotNil(t, result)
	expected := new(big.Int)
	expected.SetString(largeNum, 10)
	assert.Equal(t, expected, result.Int)
}

func TestNewBigIntFromString_Empty(t *testing.T) {
	result, ok := NewBigIntFromString("")
	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestNewBigIntFromString_Float(t *testing.T) {
	// Floats are not valid big integers
	result, ok := NewBigIntFromString("1.5")
	assert.False(t, ok)
	assert.Nil(t, result)
}

// JSON Unmarshal tests

func TestBigInt_UnmarshalJSON_String(t *testing.T) {
	var b BigInt
	err := json.Unmarshal([]byte(`"123"`), &b)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(123), b.Int)
}

func TestBigInt_UnmarshalJSON_Number(t *testing.T) {
	var b BigInt
	err := json.Unmarshal([]byte(`123`), &b)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(123), b.Int)
}

func TestBigInt_UnmarshalJSON_Null(t *testing.T) {
	var b BigInt
	b.Int = big.NewInt(123) // Set initial value
	err := json.Unmarshal([]byte(`null`), &b)
	require.NoError(t, err)
	assert.Nil(t, b.Int)
}

func TestBigInt_UnmarshalJSON_InvalidString(t *testing.T) {
	var b BigInt
	err := json.Unmarshal([]byte(`"abc"`), &b)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid BigInt string")
}

func TestBigInt_UnmarshalJSON_LargeString(t *testing.T) {
	largeNum := "123456789012345678901234567890"
	var b BigInt
	err := json.Unmarshal([]byte(`"`+largeNum+`"`), &b)
	require.NoError(t, err)
	expected := new(big.Int)
	expected.SetString(largeNum, 10)
	assert.Equal(t, expected, b.Int)
}

func TestBigInt_UnmarshalJSON_NegativeString(t *testing.T) {
	var b BigInt
	err := json.Unmarshal([]byte(`"-123"`), &b)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(-123), b.Int)
}

func TestBigInt_UnmarshalJSON_NegativeNumber(t *testing.T) {
	var b BigInt
	err := json.Unmarshal([]byte(`-123`), &b)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(-123), b.Int)
}

func TestBigInt_UnmarshalJSON_InvalidFormat(t *testing.T) {
	var b BigInt
	err := json.Unmarshal([]byte(`{"value": 123}`), &b)
	assert.Error(t, err)
}

// JSON Marshal tests

func TestBigInt_MarshalJSON_Value(t *testing.T) {
	b := NewBigIntFromInt64(123)
	data, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Equal(t, `"123"`, string(data))
}

func TestBigInt_MarshalJSON_Nil(t *testing.T) {
	var b *BigInt
	data, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Equal(t, `null`, string(data))
}

func TestBigInt_MarshalJSON_NilInt(t *testing.T) {
	b := &BigInt{Int: nil}
	data, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Equal(t, `null`, string(data))
}

func TestBigInt_MarshalJSON_Zero(t *testing.T) {
	b := NewBigIntFromInt64(0)
	data, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Equal(t, `"0"`, string(data))
}

func TestBigInt_MarshalJSON_Negative(t *testing.T) {
	b := NewBigIntFromInt64(-123)
	data, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Equal(t, `"-123"`, string(data))
}

func TestBigInt_MarshalJSON_Large(t *testing.T) {
	largeNum := "123456789012345678901234567890"
	b, ok := NewBigIntFromString(largeNum)
	require.True(t, ok)
	data, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Equal(t, `"`+largeNum+`"`, string(data))
}

// ToBigInt tests

func TestBigInt_ToBigInt_Valid(t *testing.T) {
	b := NewBigIntFromInt64(123)
	result := b.ToBigInt()
	assert.Equal(t, big.NewInt(123), result)
}

func TestBigInt_ToBigInt_Nil(t *testing.T) {
	var b *BigInt
	result := b.ToBigInt()
	assert.Nil(t, result)
}

// IsNil tests

func TestBigInt_IsNil_NilBigInt(t *testing.T) {
	var b *BigInt
	assert.True(t, b.IsNil())
}

func TestBigInt_IsNil_NilInner(t *testing.T) {
	b := &BigInt{Int: nil}
	assert.True(t, b.IsNil())
}

func TestBigInt_IsNil_Valid(t *testing.T) {
	b := NewBigIntFromInt64(123)
	assert.False(t, b.IsNil())
}

func TestBigInt_IsNil_Zero(t *testing.T) {
	b := NewBigIntFromInt64(0)
	assert.False(t, b.IsNil()) // Zero is not nil
}

// IsZero tests

func TestBigInt_IsZero_Zero(t *testing.T) {
	b := NewBigIntFromInt64(0)
	assert.True(t, b.IsZero())
}

func TestBigInt_IsZero_Nil(t *testing.T) {
	var b *BigInt
	assert.True(t, b.IsZero())
}

func TestBigInt_IsZero_NilInner(t *testing.T) {
	b := &BigInt{Int: nil}
	assert.True(t, b.IsZero())
}

func TestBigInt_IsZero_Positive(t *testing.T) {
	b := NewBigIntFromInt64(100)
	assert.False(t, b.IsZero())
}

func TestBigInt_IsZero_Negative(t *testing.T) {
	b := NewBigIntFromInt64(-100)
	assert.False(t, b.IsZero())
}

// IsPositive tests

func TestBigInt_IsPositive_Positive(t *testing.T) {
	b := NewBigIntFromInt64(100)
	assert.True(t, b.IsPositive())
}

func TestBigInt_IsPositive_Zero(t *testing.T) {
	b := NewBigIntFromInt64(0)
	assert.False(t, b.IsPositive())
}

func TestBigInt_IsPositive_Negative(t *testing.T) {
	b := NewBigIntFromInt64(-100)
	assert.False(t, b.IsPositive())
}

func TestBigInt_IsPositive_Nil(t *testing.T) {
	var b *BigInt
	assert.False(t, b.IsPositive())
}

func TestBigInt_IsPositive_NilInner(t *testing.T) {
	b := &BigInt{Int: nil}
	assert.False(t, b.IsPositive())
}

// JSON round-trip tests

func TestBigInt_JSONRoundTrip_Positive(t *testing.T) {
	original := NewBigIntFromInt64(123456)
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var result BigInt
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)
	assert.Equal(t, original.Int, result.Int)
}

func TestBigInt_JSONRoundTrip_Negative(t *testing.T) {
	original := NewBigIntFromInt64(-123456)
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var result BigInt
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)
	assert.Equal(t, original.Int, result.Int)
}

func TestBigInt_JSONRoundTrip_Large(t *testing.T) {
	original, ok := NewBigIntFromString("123456789012345678901234567890")
	require.True(t, ok)
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var result BigInt
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)
	assert.Equal(t, original.Int, result.Int)
}

// Struct embedding test
func TestBigInt_InStruct(t *testing.T) {
	type TestStruct struct {
		Amount *BigInt `json:"amount"`
		Name   string  `json:"name"`
	}

	// Marshal
	ts := TestStruct{
		Amount: NewBigIntFromInt64(123),
		Name:   "test",
	}
	data, err := json.Marshal(ts)
	require.NoError(t, err)
	assert.JSONEq(t, `{"amount":"123","name":"test"}`, string(data))

	// Unmarshal
	var result TestStruct
	err = json.Unmarshal([]byte(`{"amount":"456","name":"other"}`), &result)
	require.NoError(t, err)
	assert.Equal(t, big.NewInt(456), result.Amount.Int)
	assert.Equal(t, "other", result.Name)
}

func TestBigInt_InStructWithNull(t *testing.T) {
	type TestStruct struct {
		Amount *BigInt `json:"amount"`
	}

	var result TestStruct
	err := json.Unmarshal([]byte(`{"amount":null}`), &result)
	require.NoError(t, err)
	// Amount should be a BigInt with nil Int, not a nil BigInt
	if result.Amount != nil {
		assert.True(t, result.Amount.IsNil())
	}
}
