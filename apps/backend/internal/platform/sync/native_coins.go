package sync

// NativeCoinGeckoID maps a chain to the CoinGecko slug of its native coin
// (issue #59, decision #39).
//
// WHY THIS EXISTS. Native coins were locked out of pricing by three gates. Two
// of them — the "contract is non-empty" check in the transaction builder and the
// partial uniqueness index that excluded natives from the asset store — are paid
// for by the `native` sentinel from #56 and disappear with the old tables. The
// third is the price providers themselves: DefiLlama is keyed on
// (chain, contract) and has no key for a chain's own coin, while CoinGecko needs
// a coin slug it cannot derive from an address. So there has to be a place that
// says "the native coin of Base is ether"; this is that place.
//
// The consequence of not having it is not cosmetic. Gas is paid in the native
// coin on every transaction, and the native coin is the largest position in most
// wallets, so an unpriceable native means a permanent zero flowing straight into
// cost basis and PnL — silently, because a zero price is indistinguishable from
// a cheap asset once written.
//
// WHY A HARDCODED TABLE IS NOT TECHNICAL DEBT HERE. The set of chains MoonTrack
// syncs is closed and enumerated in the chain registry; a chain's native coin
// never changes, and adding a chain already requires a code change. A lookup
// table whose keys are fixed and whose values are immutable is a constant, not a
// deferred decision. Making it configuration would add a way to get it wrong at
// runtime with no corresponding flexibility gained.
//
// WHY NOT SUBSTITUTE THE WRAPPED CONTRACT. Pricing ETH as WETH was rejected in
// #39. The two are not the same asset: they have different identities under
// (chain, contract), they de-peg under stress, and a wrapper's price is a claim
// about the wrapper. Booking a native movement against a token's identity would
// reintroduce the exact confusion this epic removes, and it would do it inside
// the pricing path where it is hardest to see.
// The keys are DOMAIN chain slugs — the vocabulary used throughout MoonTrack
// and stored in asset_registry.chain — not the provider's short slugs. The two
// differ (`binance-smart-chain` vs `bsc`), and the registry stores the domain
// form, so keying this table on the provider form would miss every lookup.
//
// The set is exactly the chains the sync provider supports (see the chain map in
// the Noves gateway). A chain absent from that map cannot produce a native leg
// in the first place.
var nativeCoinGeckoID = map[string]string{
	"ethereum":            "ethereum",
	"base":                "ethereum",
	"arbitrum":            "ethereum",
	"optimism":            "ethereum",
	"polygon":             "matic-network",
	"binance-smart-chain": "binancecoin",
	"avalanche":           "avalanche-2",
}

// NativeCoinGeckoID returns the CoinGecko slug for a chain's native coin, and
// whether one is known.
//
// Base, Arbitrum and Optimism all return "ethereum": they are L2s whose native
// coin IS ether, so they share a price feed while remaining three distinct
// registry rows. That is the composite key behaving as designed — the identity
// is per chain, the quote is per coin, and the two are allowed to differ.
//
// An unknown chain returns false rather than a guess. The caller leaves
// coingecko_id NULL, which costs that chain's native coin its price until the
// chain is added here — visibly, via a resolvable-but-unpriced asset, rather
// than by quoting it as something it is not.
func NativeCoinGeckoID(chain string) (string, bool) {
	id, ok := nativeCoinGeckoID[chain]
	return id, ok
}
