// Package accountcode is the single producer of ledger account codes.
//
// Every production site that builds an account code calls one of the
// constructors below. Centralising construction is step 1 of the two-step
// route to changing the code shape (#55, decision in #36): once every producer
// goes through here, the shape can be changed later by editing one file.
//
// # Why a function per namespace, not one constructor with variants
//
// The arity is the contract. Each namespace takes exactly the arguments it
// needs, so mixing two namespaces up is a compile error rather than a string
// that happens to look plausible. A single constructor taking options or a
// variadic tail would collapse that arity into runtime and lose precisely the
// protection this package exists to provide. walletID is typed uuid.UUID for
// the same reason: it rules out wallet.invalid-uuid-format.BTC at compile time.
//
// # Boundary: these functions return a string and nothing else
//
// Account codes travel to the ledger inside Entry.Metadata["account_code"], and
// that metadata also carries wallet_id, chain_id and sometimes an explicit
// account_type. Those are separate knowledge — the ledger's resolver reads them
// to decide what kind of account to create. This package does not touch them.
// Folding that decision in here would be a change of semantics, not a
// refactor.
//
// # Parsing deliberately lives elsewhere
//
// Reading a code back — inferring an AccountType from its prefix — stays in
// internal/ledger. It produces ledger.AccountType, so moving it here would make
// this package depend on ledger and invert the layering. Dependencies here are
// uuid and nothing more.
package accountcode

import (
	"github.com/google/uuid"
)

// WalletCode addresses the asset balance a wallet holds on a chain.
//
//	wallet.{walletID}.{chain}.{asset}
func WalletCode(walletID uuid.UUID, chain, asset string) string {
	return "wallet." + walletID.String() + "." + chain + "." + asset
}

// IncomeCode is the generic income account for an inbound asset.
//
//	income.{chain}.{asset}
func IncomeCode(chain, asset string) string {
	return "income." + chain + "." + asset
}

// IncomeGenesisCode books an opening balance: value that entered the portfolio
// before tracking began and therefore has no acquiring transaction.
//
//	income.genesis.{chain}.{asset}
func IncomeGenesisCode(chain, asset string) string {
	return "income.genesis." + chain + "." + asset
}

// IncomeLpCode books income earned from a liquidity position.
//
//	income.lp.{chain}.{asset}
func IncomeLpCode(chain, asset string) string {
	return "income.lp." + chain + "." + asset
}

// IncomeDefiCode books income claimed from a DeFi protocol.
//
//	income.defi.{chain}.{asset}
func IncomeDefiCode(chain, asset string) string {
	return "income.defi." + chain + "." + asset
}

// IncomeLendingCode books rewards claimed from a lending protocol.
//
//	income.lending.{chain}.{asset}
func IncomeLendingCode(chain, asset string) string {
	return "income.lending." + chain + "." + asset
}

// ExpenseCode books an asset leaving the portfolio as an expense.
//
//	expense.{chain}.{asset}
func ExpenseCode(chain, asset string) string {
	return "expense." + chain + "." + asset
}

// GasCode books a transaction fee paid in a chain's asset.
//
//	gas.{chain}.{asset}
func GasCode(chain, asset string) string {
	return "gas." + chain + "." + asset
}

// ClearingCode is the transit account that holds one leg of a multi-leg
// operation (swap, liquidity add/remove) so each pair stays balanced.
//
//	clearing.{chain}.{asset}
func ClearingCode(chain, asset string) string {
	return "clearing." + chain + "." + asset
}

// CollateralCode addresses assets a wallet has supplied to a lending protocol.
// Unlike the wallet namespace it is scoped by protocol, because the same asset
// supplied to two protocols is two distinct positions.
//
//	collateral.{proto}.{walletID}.{chain}.{asset}
func CollateralCode(proto string, walletID uuid.UUID, chain, asset string) string {
	return "collateral." + proto + "." + walletID.String() + "." + chain + "." + asset
}

// LiabilityCode addresses a debt a wallet owes a lending protocol, scoped by
// protocol for the same reason as CollateralCode.
//
//	liability.{proto}.{walletID}.{chain}.{asset}
func LiabilityCode(proto string, walletID uuid.UUID, chain, asset string) string {
	return "liability." + proto + "." + walletID.String() + "." + chain + "." + asset
}
