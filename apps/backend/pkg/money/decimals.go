package money

import "strings"

// GetDecimals returns the number of decimal places for an asset by symbol or CoinGecko ID.
// Returns 8 as default for unknown assets.
func GetDecimals(assetID string) int {
	if d, ok := knownDecimals[strings.ToLower(assetID)]; ok {
		return d
	}
	return 8
}

var knownDecimals = map[string]int{
	// Native symbols
	"btc":   8,
	"eth":   18,
	"usdt":  6,
	"usdc":  6,
	"sol":   9,
	"bnb":   18,
	"xrp":   6,
	"ada":   6,
	"doge":  8,
	"matic": 18,
	"dot":   10,
	"avax":  18,
	"link":  18,
	"trx":   6,
	"dai":   18,
	"wbtc":  8,
	"ltc":   8,
	"bch":   8,
	"ton":   9,
	"shib":  18,
	// CoinGecko IDs
	"bitcoin":          8,
	"ethereum":         18,
	"tether":           6,
	"usd-coin":         6,
	"solana":           9,
	"binancecoin":      18,
	"ripple":           6,
	"cardano":          6,
	"dogecoin":         8,
	"matic-network":    18,
	"polkadot":         10,
	"avalanche-2":      18,
	"chainlink":        18,
	"tron":             6,
	"litecoin":         8,
	"bitcoin-cash":     8,
	"the-open-network": 9,
	"shiba-inu":        18,
	"wrapped-bitcoin":  8,
}
