package noves

// Chain-slug mapping between the domain vocabulary (Zerion-style slugs used
// throughout MoonTrack: "ethereum", "binance-smart-chain", …) and the Noves
// short slugs ("eth", "bsc", …). Only these EVM chains are supported; most map
// 1:1, but ethereum↔eth and binance-smart-chain↔bsc differ.
var domainToNoves = map[string]string{
	"ethereum":            "eth",
	"binance-smart-chain": "bsc",
	"base":                "base",
	"arbitrum":            "arbitrum",
	"polygon":             "polygon",
	"optimism":            "optimism",
	"avalanche":           "avalanche",
}

// novesToDomain is the inverse of domainToNoves.
var novesToDomain = func() map[string]string {
	m := make(map[string]string, len(domainToNoves))
	for d, n := range domainToNoves {
		m[n] = d
	}
	return m
}()

// domainToNovesChain maps a domain chain slug to the Noves short slug used in
// the endpoint. The bool is false for unsupported chains.
func domainToNovesChain(domain string) (string, bool) {
	n, ok := domainToNoves[domain]
	return n, ok
}

// novesToDomainChain maps a Noves short slug back to the canonical domain slug.
// The bool is false for unknown Noves slugs.
func novesToDomainChain(noves string) (string, bool) {
	d, ok := novesToDomain[noves]
	return d, ok
}
