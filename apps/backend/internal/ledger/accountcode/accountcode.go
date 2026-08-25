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
// # Two forms during the migration
//
// Every namespace has two constructors: the original one taking chain and asset
// as independent strings (WalletCode, IncomeCode, …) and one taking the pair as
// a single [Asset] value (Wallet, Income, …). The second form is the target —
// it makes a chain/asset pair that came from two different assets unbuildable,
// which is the defect in #70 — and callers move onto it namespace by namespace
// (#83, #84) before the string form is removed (#85). Both emit byte-identical
// codes; new code should take the [Asset] form. See asset.go.
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
	"strings"

	"github.com/google/uuid"
)

// UnknownProtocol is the segment used when a protocol-scoped code is built
// without a protocol name.
//
// It exists because an empty segment is not a neutral choice: it collapses
// "collateral..{wallet}.{chain}.{asset}" and produces a code that still has
// five segments but names nothing, and — worse — is a *different string* from
// the code the same position gets once the provider does supply a name. That
// split the same lending position across two accounts on live data: a supply
// landed on the named account, the matching withdraw looked for the empty one,
// found a zero balance and failed the whole transaction (#73). A visible
// sentinel keeps the arity constant and makes the missing name legible in the
// code itself rather than as a hole between two dots.
const UnknownProtocol = "unknown"

// protocolSlug normalises a provider-supplied protocol name into a stable
// account-code segment.
//
// Two properties matter, and they are the reason this lives in the constructor
// rather than in each caller:
//
//   - The result carries no dot. Account codes are parsed by splitting on '.',
//     so a name like "Aave v3.1" would silently add a segment and change the
//     code's arity — collateral.aave-v3.1.{wallet}… reads as six segments, and
//     any consumer counting them is then wrong about which field is which.
//   - The mapping is many-to-one and idempotent. "Fluid USD Coin", "fluid usd
//     coin" and "Fluid  USD  Coin" all become "fluid-usd-coin", so the same
//     position cannot land on two accounts because the provider changed its
//     capitalisation or spacing between two syncs.
//
// The alphabet is [a-z0-9-]: letters and digits are lowercased and kept, every
// other rune becomes a separator, runs of separators collapse to one dash, and
// leading/trailing dashes are trimmed. A name that contains nothing usable
// (punctuation only) degrades to UnknownProtocol rather than to an empty
// segment, for the same reason an absent name does.
func protocolSlug(proto string) string {
	var b strings.Builder
	b.Grow(len(proto))
	pendingDash := false

	for _, r := range proto {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r - 'A' + 'a')
		default:
			// Any other rune — space, dot, slash, non-ASCII letter — is a
			// separator. Deferring the dash until the next kept rune is what
			// collapses runs and drops trailing ones.
			pendingDash = true
		}
	}

	if b.Len() == 0 {
		return UnknownProtocol
	}
	return b.String()
}

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
// The protocol segment is normalised by protocolSlug; chain and asset are
// still passed through verbatim, since those are internal identifiers rather
// than provider display names.
//
//	collateral.{proto}.{walletID}.{chain}.{asset}
func CollateralCode(proto string, walletID uuid.UUID, chain, asset string) string {
	return "collateral." + protocolSlug(proto) + "." + walletID.String() + "." + chain + "." + asset
}

// LiabilityCode addresses a debt a wallet owes a lending protocol, scoped by
// protocol for the same reason as CollateralCode.
//
//	liability.{proto}.{walletID}.{chain}.{asset}
func LiabilityCode(proto string, walletID uuid.UUID, chain, asset string) string {
	return "liability." + protocolSlug(proto) + "." + walletID.String() + "." + chain + "." + asset
}
