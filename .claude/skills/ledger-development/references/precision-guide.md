# Financial Precision Guide

MoonTrack handles cryptocurrency amounts with extreme precision using `NUMERIC(78,0)` in PostgreSQL and `*big.Int` in Go. This guide explains how to work with these types correctly.

## Why NUMERIC(78,0)?

Cryptocurrencies use different decimal precisions:

| Asset | Smallest Unit | Decimals | Max Value Example |
|-------|---------------|----------|-------------------|
| Bitcoin | Satoshi | 8 | 2.1 * 10^15 satoshis |
| Ethereum | Wei | 18 | 10^77 wei is possible |
| Solana | Lamport | 9 | ~10^18 lamports |

`NUMERIC(78,0)` can store:
- Up to 78 digits (no decimal point)
- Range: 0 to 10^78 - 1
- Covers all cryptocurrency amounts including dust and whale holdings

## Never Use float64

**Why float64 is dangerous:**

```go
// WRONG: Precision loss with float64
amount := 0.1 + 0.2
fmt.Println(amount) // Output: 0.30000000000000004

// WRONG: Large number precision loss
bigAmount := float64(115792089237316195423570985008687907853269984665640564039457584007913129639935)
fmt.Println(bigAmount) // Loses precision!
```

**Always use big.Int:**

```go
// CORRECT: No precision loss
amount1 := big.NewInt(100000000000000000) // 0.1 ETH in wei
amount2 := big.NewInt(200000000000000000) // 0.2 ETH in wei
total := new(big.Int).Add(amount1, amount2)
fmt.Println(total.String()) // Output: 300000000000000000 (exactly 0.3 ETH)
```

## Working with big.Int

### Creating big.Int Values

```go
// From int64 (for small values only)
small := big.NewInt(100)

// From string (preferred for large values)
large, ok := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
if !ok {
    return errors.New("invalid number string")
}

// From hex string (common for blockchain values)
hexValue, ok := new(big.Int).SetString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)
```

### Arithmetic Operations

```go
a := big.NewInt(100)
b := big.NewInt(50)

// Addition
sum := new(big.Int).Add(a, b)  // 150

// Subtraction
diff := new(big.Int).Sub(a, b)  // 50

// Multiplication
product := new(big.Int).Mul(a, b)  // 5000

// Division
quotient := new(big.Int).Div(a, b)  // 2

// Modulo
remainder := new(big.Int).Mod(a, b)  // 0
```

### Comparison

```go
a := big.NewInt(100)
b := big.NewInt(50)

// Compare returns -1, 0, or 1
a.Cmp(b)  // 1 (a > b)
b.Cmp(a)  // -1 (b < a)
a.Cmp(a)  // 0 (equal)

// Sign returns -1, 0, or 1
a.Sign()  // 1 (positive)
big.NewInt(0).Sign()  // 0
big.NewInt(-1).Sign()  // -1 (negative)
```

### Copying Values

```go
original := big.NewInt(100)

// WRONG: Both point to same underlying data
wrong := original
wrong.Add(wrong, big.NewInt(1))  // Modifies original!

// CORRECT: Create independent copy
correct := new(big.Int).Set(original)
correct.Add(correct, big.NewInt(1))  // Doesn't affect original
```

## USD Rate Scaling

USD rates are stored scaled by 10^8 to preserve precision:

```go
const USDRateScale = 100_000_000  // 10^8

// $50,000.00 USD per BTC
usdRate := big.NewInt(5_000_000_000_000)  // 50000 * 10^8

// Calculate USD value
// Formula: usdValue = (amount * usdRate) / 10^8
amount := big.NewInt(100_000_000)  // 1 BTC in satoshis
scale := big.NewInt(USDRateScale)

usdValue := new(big.Int).Mul(amount, usdRate)
usdValue.Div(usdValue, scale)
// Result: 50000 * 10^8 / 10^8 = $50,000
```

### Price Source Tracking

Always record where the price came from:

```go
entry := &ledger.Entry{
    Amount:   amount,
    USDRate:  usdRate,
    USDValue: usdValue,
    Metadata: map[string]interface{}{
        "price_source":    "coingecko",
        "price_timestamp": time.Now().Format(time.RFC3339),
    },
}
```

## Database Operations

### Storing big.Int in NUMERIC

PostgreSQL's `pgx` driver handles big.Int automatically:

```go
// Insert
amount := new(big.Int)
amount.SetString("1000000000000000000", 10)

_, err := pool.Exec(ctx, `
    INSERT INTO entries (amount) VALUES ($1)
`, amount)

// Select
var retrievedAmount big.Int
err := pool.QueryRow(ctx, `
    SELECT amount FROM entries WHERE id = $1
`, id).Scan(&retrievedAmount)
```

### NULL Handling

```go
// For nullable NUMERIC columns
var balance *big.Int  // nil represents NULL

// Scanning NULL
row := pool.QueryRow(ctx, query)
err := row.Scan(&balance)
if balance == nil {
    // Handle NULL case
}

// Inserting NULL
_, err := pool.Exec(ctx, `
    INSERT INTO table (balance) VALUES ($1)
`, nil)  // Inserts NULL
```

## Common Conversion Functions

### Satoshi to BTC (for display)

```go
func SatoshiToBTC(satoshi *big.Int) string {
    btc := new(big.Rat).SetFrac(satoshi, big.NewInt(100_000_000))
    return btc.FloatString(8)  // "1.00000000"
}
```

### Wei to ETH (for display)

```go
func WeiToETH(wei *big.Int) string {
    eth := new(big.Rat).SetFrac(wei, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
    return eth.FloatString(18)
}
```

### String to big.Int (from API input)

```go
func ParseAmount(s string) (*big.Int, error) {
    amount := new(big.Int)
    _, ok := amount.SetString(s, 10)
    if !ok {
        return nil, fmt.Errorf("invalid amount: %s", s)
    }
    if amount.Sign() < 0 {
        return nil, fmt.Errorf("negative amount not allowed")
    }
    return amount, nil
}
```

## Test Values for Precision

Use these values in tests to verify precision handling:

```go
var precisionTestCases = []struct {
    name  string
    value string
}{
    // Edge cases
    {"zero", "0"},
    {"one satoshi", "1"},
    {"one wei", "1"},

    // Common values
    {"1 BTC in satoshi", "100000000"},
    {"1 ETH in wei", "1000000000000000000"},
    {"1 SOL in lamports", "1000000000"},

    // Large values
    {"10 billion ETH in wei", "10000000000000000000000000000"},
    {"max uint64", "18446744073709551615"},
    {"max uint128", "340282366920938463463374607431768211455"},
    {"max uint256", "115792089237316195423570985008687907853269984665640564039457584007913129639935"},

    // Near NUMERIC(78,0) limit
    {"10^77", "100000000000000000000000000000000000000000000000000000000000000000000000000000"},
    {"10^78 - 1", "999999999999999999999999999999999999999999999999999999999999999999999999999999"},
}

func TestPrecision(t *testing.T) {
    for _, tc := range precisionTestCases {
        t.Run(tc.name, func(t *testing.T) {
            original, ok := new(big.Int).SetString(tc.value, 10)
            require.True(t, ok)

            // Store in DB
            err := storeAmount(ctx, original)
            require.NoError(t, err)

            // Retrieve from DB
            retrieved, err := getAmount(ctx)
            require.NoError(t, err)

            // Must be exactly equal
            assert.Equal(t, 0, original.Cmp(retrieved),
                "Precision lost: original=%s, retrieved=%s",
                original.String(), retrieved.String())
        })
    }
}
```

## JSON Serialization

big.Int doesn't serialize to JSON by default. Use string representation:

```go
type TransactionResponse struct {
    Amount string `json:"amount"`  // Store as string
}

// Serialize
response := TransactionResponse{
    Amount: amount.String(),
}

// Deserialize
amount, ok := new(big.Int).SetString(response.Amount, 10)
```

Or use a custom type:

```go
type BigInt struct {
    *big.Int
}

func (b BigInt) MarshalJSON() ([]byte, error) {
    return []byte(`"` + b.String() + `"`), nil
}

func (b *BigInt) UnmarshalJSON(data []byte) error {
    str := strings.Trim(string(data), `"`)
    b.Int = new(big.Int)
    _, ok := b.SetString(str, 10)
    if !ok {
        return fmt.Errorf("invalid big.Int: %s", str)
    }
    return nil
}
```

## Common Mistakes

### 1. Losing Precision in Division

```go
// WRONG: Integer division loses remainder
a := big.NewInt(10)
b := big.NewInt(3)
result := new(big.Int).Div(a, b)  // 3, not 3.333...

// If you need fractional results, use big.Rat
rat := new(big.Rat).SetFrac(a, b)  // 10/3
```

### 2. Modifying Shared Values

```go
// WRONG: entry.Amount is modified
func process(entry *Entry) {
    doubled := entry.Amount.Mul(entry.Amount, big.NewInt(2))  // Modifies entry.Amount!
}

// CORRECT: Create a copy first
func process(entry *Entry) {
    doubled := new(big.Int).Mul(entry.Amount, big.NewInt(2))  // entry.Amount unchanged
}
```

### 3. Using int64 for Large Values

```go
// WRONG: Overflow for values > 9.2 * 10^18
amount := int64(10000000000000000000)  // Overflow!

// CORRECT: Use big.Int
amount, _ := new(big.Int).SetString("10000000000000000000", 10)
```

### 4. Comparing with ==

```go
// WRONG: Compares pointers, not values
if amount1 == amount2 { ... }

// CORRECT: Use Cmp
if amount1.Cmp(amount2) == 0 { ... }
```

## Summary

| Operation | Correct Approach |
|-----------|-----------------|
| Store amounts | NUMERIC(78,0) in DB, *big.Int in Go |
| Parse from string | `big.Int.SetString(s, 10)` |
| Arithmetic | Use big.Int methods (Add, Sub, Mul, Div) |
| Comparison | Use `Cmp()` method, not == |
| Copy values | Use `new(big.Int).Set(original)` |
| JSON | Serialize as string |
| USD rates | Scale by 10^8 |
| Display | Convert to Rat for decimal representation |
