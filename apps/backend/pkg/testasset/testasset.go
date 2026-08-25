// Package testasset provides fixed registry asset ids for tests.
//
// After #59 an asset's identity in the ledger is an asset_registry UUID, so a
// test that used to write "ETH" now has to write a UUID. Two things would go
// wrong if each test invented its own:
//
//   - uuid.New() per call makes a test that books two entries "in ETH" book them
//     against two different assets, and the balance check that should catch it
//     passes anyway because both sides balance.
//   - a UUID literal typed out inline is unreadable, so a copy-paste slip
//     between two tickers is invisible in review.
//
// The ids here are fixed, so the same ticker is the same asset in every test
// and across runs, and named, so the intent reads at the call site. The v4 shape
// is deliberate: these must be valid UUIDs, because production code parses them.
//
// These are NOT real registry rows. Nothing seeds them into asset_registry —
// they are identities for unit tests that never reach the database. An
// integration test that needs a row must create one and use the id it gets back.
package testasset

import "github.com/google/uuid"

// namespace is a fixed v4 UUID used only as the seed for ForTicker. It is
// arbitrary; what matters is that it never changes, so a ticker maps to the same
// id in every test and every run.
var namespace = uuid.MustParse("5f7e2c1a-0000-4000-8000-000000000000")

// ForTicker returns a stable registry id for an arbitrary ticker.
//
// The named vars below cover the majors, but the DeFi tests exercise long tails
// — cbBTC, aBascbBTC, stkAAVE, GM-ETH-USDC — where the ticker itself is the
// point of the case (a receipt token beside its principal). Naming every one of
// those would bury the list; deriving them keeps the ticker readable at the call
// site while still giving each a distinct, repeatable id.
//
// Derivation is UUIDv5 over a fixed namespace, so it is a pure function of the
// ticker: same string, same id, every run and every package. Distinct strings
// get distinct ids, which is the property these tests actually rely on — a
// receipt must not collide with the asset it tracks (#59).
func ForTicker(ticker string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte(ticker))
}

// Fixed ids for the tickers the module tests use. The numeric prefix of each is
// a hint at the ticker so a mismatch is visible without decoding the UUID.
var (
	ETH   = uuid.MustParse("e0000000-0000-4000-8000-000000000001")
	WETH  = uuid.MustParse("e0000000-0000-4000-8000-000000000002")
	USDC  = uuid.MustParse("c0000000-0000-4000-8000-000000000001")
	USDT  = uuid.MustParse("c0000000-0000-4000-8000-000000000002")
	DAI   = uuid.MustParse("c0000000-0000-4000-8000-000000000003")
	BTC   = uuid.MustParse("b0000000-0000-4000-8000-000000000001")
	WBTC  = uuid.MustParse("b0000000-0000-4000-8000-000000000002")
	AAVE  = uuid.MustParse("a0000000-0000-4000-8000-000000000001")
	MATIC = uuid.MustParse("a0000000-0000-4000-8000-000000000002")
	BNB   = uuid.MustParse("a0000000-0000-4000-8000-000000000003")
	CRV   = uuid.MustParse("a0000000-0000-4000-8000-000000000004")
	UNI   = uuid.MustParse("a0000000-0000-4000-8000-000000000005")
	ARB   = uuid.MustParse("a0000000-0000-4000-8000-000000000006")
	LINK  = uuid.MustParse("a0000000-0000-4000-8000-000000000007")
)

// ETHOnArbitrum is ETH as a SECOND asset: the same ticker on another chain.
//
// Identity is (chain, contract) since #59, so a token that exists on two chains
// is two registry rows with two ids — and a bridge moves value between them.
// A test that used ETH on both sides of a bridge was asserting the shape of
// #70, where the destination account was built from the destination chain and
// the source asset's id, and it made the tax-lot carry-over look like it worked
// for a reason that was never true (#84).
var ETHOnArbitrum = uuid.MustParse("e0000000-0000-4000-8000-000000000003")
