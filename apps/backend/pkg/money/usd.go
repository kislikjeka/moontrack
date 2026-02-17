package money

import "math/big"

// CalcUSDValue computes (amount * usdRate) / 10^decimals — result is USD value scaled by 10^8
func CalcUSDValue(amount, usdRate *big.Int, decimals int) *big.Int {
	if usdRate == nil || usdRate.Sign() == 0 {
		return big.NewInt(0)
	}
	value := new(big.Int).Mul(amount, usdRate)
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	value.Div(value, divisor)
	return value
}
