package accountcode

import "github.com/google/uuid"

// Asset is the identity of an asset as an account code addresses it: the chain
// it lives on together with its UUID in the asset registry.
//
// # Why the pair is one value
//
// The two halves are not independent. Asset identity in this system is
// (chain, contract) — the same token on another chain is another asset — and
// the registry UUID already encodes that pair. A chain segment and a UUID that
// came from different assets therefore name nothing: the code is well-formed,
// five segments and a plausible UUID, and it addresses an account that should
// not exist.
//
// That is not hypothetical. In #70 a cross-chain transfer built a code from the
// destination chain and the source asset's UUID, so one asset ended up
// addressed by two accounts. Their balances **sum to a plausible number**, which
// is why no amount check caught it — only a cardinality check ("exactly one
// account per identity") did, and only after the entries were written.
//
// A runtime check would catch such a pair late and only where someone
// remembered to write it. Holding the pair as a value makes the mismatched pair
// unbuildable instead: the fields are unexported and there is no setter, so
// once an Asset exists neither half can be swapped for the other's. The only
// way to obtain one is [OnChain], which demands both parts at once.
//
// # Why the type lives here and not elsewhere
//
//   - Not platform/sync's AssetKey: that one is keyed by contract address
//     rather than registry UUID, and it sits in a layer this package must not
//     depend on (transport → module → platform → ledger ← infra).
//   - Not internal/ledger: that would make accountcode depend on ledger and
//     invert the layering, for the same reason code *parsing* deliberately
//     stays there and not here.
//
// The package stays leaf: uuid and nothing more.
type Asset struct {
	chain string
	id    uuid.UUID
}

// OnChain pairs a chain with the registry UUID of an asset on that chain.
//
// It is the only way to build an [Asset]. Both parts are required by arity, the
// same contract the constructors below rely on: there is no partial form to
// fill in later and no field to overwrite afterwards.
//
// Neither part is validated. Whether the UUID really belongs to that chain is
// registry knowledge, and reaching for it here would make this leaf package
// depend on the platform layer. What this type guarantees is narrower and
// enough: whatever pair a caller reads, it travels to every constructor
// together and unaltered.
func OnChain(chain string, id uuid.UUID) Asset {
	return Asset{chain: chain, id: id}
}

// seg renders the trailing "{chain}.{asset}" of a code.
//
// Every constructor below ends with this pair, which is the whole point: one
// place decides how an identity becomes two segments, so the halves cannot
// drift apart between namespaces.
func (a Asset) seg() string {
	return a.chain + "." + a.id.String()
}

// Wallet addresses the asset balance a wallet holds on a chain.
//
//	wallet.{walletID}.{chain}.{asset}
func Wallet(walletID uuid.UUID, asset Asset) string {
	return "wallet." + walletID.String() + "." + asset.seg()
}

// Income is the generic income account for an inbound asset.
//
//	income.{chain}.{asset}
func Income(asset Asset) string {
	return "income." + asset.seg()
}

// IncomeGenesis books an opening balance: value that entered the portfolio
// before tracking began and therefore has no acquiring transaction.
//
//	income.genesis.{chain}.{asset}
func IncomeGenesis(asset Asset) string {
	return "income.genesis." + asset.seg()
}

// IncomeLp books income earned from a liquidity position.
//
//	income.lp.{chain}.{asset}
func IncomeLp(asset Asset) string {
	return "income.lp." + asset.seg()
}

// IncomeDefi books income claimed from a DeFi protocol.
//
//	income.defi.{chain}.{asset}
func IncomeDefi(asset Asset) string {
	return "income.defi." + asset.seg()
}

// IncomeLending books rewards claimed from a lending protocol.
//
//	income.lending.{chain}.{asset}
func IncomeLending(asset Asset) string {
	return "income.lending." + asset.seg()
}

// Expense books an asset leaving the portfolio as an expense.
//
//	expense.{chain}.{asset}
func Expense(asset Asset) string {
	return "expense." + asset.seg()
}

// Gas books a transaction fee paid in a chain's asset.
//
//	gas.{chain}.{asset}
func Gas(asset Asset) string {
	return "gas." + asset.seg()
}

// Clearing is the transit account that holds one leg of a multi-leg operation
// (swap, liquidity add/remove) so each pair stays balanced.
//
//	clearing.{chain}.{asset}
func Clearing(asset Asset) string {
	return "clearing." + asset.seg()
}

// Collateral addresses assets a wallet has supplied to a lending protocol.
//
// The protocol stays a separate argument: it is not part of the asset's
// identity but a second scope on top of it — the same asset supplied to two
// protocols is two distinct positions. Only the chain/asset pair folds.
//
//	collateral.{proto}.{walletID}.{chain}.{asset}
func Collateral(proto string, walletID uuid.UUID, asset Asset) string {
	return "collateral." + protocolSlug(proto) + "." + walletID.String() + "." + asset.seg()
}

// Liability addresses a debt a wallet owes a lending protocol, scoped by
// protocol for the same reason as [Collateral].
//
//	liability.{proto}.{walletID}.{chain}.{asset}
func Liability(proto string, walletID uuid.UUID, asset Asset) string {
	return "liability." + protocolSlug(proto) + "." + walletID.String() + "." + asset.seg()
}
