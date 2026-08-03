package reconcilereport

import (
	"context"
	"math/big"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// LedgerAccount is one wallet-namespace ledger account, carried with BOTH ways
// of stating its balance so the report can compare them against each other.
//
// The two are separate fields rather than one "balance" because keeping them
// apart is the entire content of check 2. Collapsing them would leave the report
// unable to tell a posting error from a stale materialization, which is the
// difference between "the ledger is wrong" and "the cache is wrong".
type LedgerAccount struct {
	AccountID uuid.UUID
	Code      string

	// Type is the ledger account type. It decides which checks an account takes
	// part in, and the split is not cosmetic.
	//
	// Checks 2 and 3 cover EVERY account the wallet owns, collateral and
	// liability included: #49's evidence for the defect it was diagnosing was
	// literally an asset sitting in `collateral..{walletID}.base.…`, so a
	// materialization check or a split check that skipped that namespace would
	// be blind to the case that motivated it.
	//
	// The TRIANGLE's L is the wallet namespace alone, because P cannot see
	// anything else: the provider's balancesOf reports what the wallet ADDRESS
	// holds, and an asset supplied to a lending market has left that address.
	// Summing collateral into L would make every supplied position read as a
	// balance the provider fails to report.
	Type string

	// Key is the asset identity this account addresses, resolved through the
	// asset registry (#59): the account's asset_id is a registry UUID, and the
	// registry row carries (chain, contract).
	Key    sync.AssetKey
	Symbol string

	// EntriesBalance is SUM(debit) − SUM(credit) over this account's entries.
	// This is L. It is taken from the ENTRIES and never from account_balances,
	// because a stale materialization read as L would be reported as a posting
	// error — the diagnosis would name the wrong defect, which is precisely what
	// the triangle exists to avoid.
	EntriesBalance *big.Int

	// MaterializedBalance is what account_balances holds for this account, and
	// nil when it holds no row at all. A missing row is not the same fact as a
	// zero row: one means the materialization never happened, the other means it
	// happened and produced zero.
	MaterializedBalance *big.Int
}

// LedgerSnapshot is everything the three networkless checks read.
//
// It is a value, loaded once, rather than a live handle. The checks are pure
// functions of it, which is what makes them testable without a database and
// what makes "checks 1–3 need no network" a property of the code rather than a
// claim in a comment.
type LedgerSnapshot struct {
	WalletID uuid.UUID

	// Accounts holds every wallet-namespace account for the wallet, INCLUDING
	// those whose balance is zero. Zero-balance accounts still take part in
	// checks 2 and 3: a split account pair can sum to the right total with one
	// side at zero, and dropping zeros would hide it.
	Accounts []LedgerAccount

	// ChainCursors maps chain slug to the moment collection reached, taken from
	// wallet_chain_sync.collect_cursor_at. It is one half of the time gap the
	// report prints instead of pretending to close.
	ChainCursors map[string]string
}

// LedgerReader loads the ledger side of the report.
//
// It is an interface so the checks can be exercised against constructed
// ledgers — a split account, a stale materialization — which is the only way
// the mandatory tests can produce those states at all. The production
// implementation is a handful of SQL queries in the command.
type LedgerReader interface {
	Load(ctx context.Context, walletID uuid.UUID) (*LedgerSnapshot, error)
}

// LedgerBalances aggregates the wallet's entries-derived balances by asset
// identity — the L column of the triangle.
//
// Aggregation by (chain, contract) is what the comparison needs and also what
// makes check 3 indispensable: two accounts on one identity, summing to the
// right total, are indistinguishable from one correct account HERE. That is why
// splitting is caught by counting accounts and never by comparing amounts.
// It covers the WALLET namespace only. The provider's balancesOf reports what
// the wallet ADDRESS holds, and an asset supplied to a lending market has left
// that address — so summing collateral into L would turn every supplied position
// into a balance the provider appears to be missing. Checks 2 and 3 see the
// other namespaces; the triangle deliberately does not.
func (s LedgerSnapshot) LedgerBalances() map[sync.AssetKey]*big.Int {
	out := make(map[sync.AssetKey]*big.Int, len(s.Accounts))
	for _, a := range s.Accounts {
		if a.Type != AccountTypeCryptoWallet {
			continue
		}
		sum, ok := out[a.Key]
		if !ok {
			sum = big.NewInt(0)
			out[a.Key] = sum
		}
		if a.EntriesBalance != nil {
			sum.Add(sum, a.EntriesBalance)
		}
	}
	return out
}

// AccountTypeCryptoWallet is the ledger type of an account holding an asset in
// the wallet address itself — the only namespace the provider's balances
// endpoint can observe, and therefore the only one comparable against P.
const AccountTypeCryptoWallet = "CRYPTO_WALLET"

// Symbols maps each asset identity to a display symbol, taken from the registry
// row behind the account. Metadata only — never an identifier.
func (s LedgerSnapshot) Symbols() map[sync.AssetKey]string {
	out := make(map[sync.AssetKey]string, len(s.Accounts))
	for _, a := range s.Accounts {
		if _, seen := out[a.Key]; !seen && a.Symbol != "" {
			out[a.Key] = a.Symbol
		}
	}
	return out
}
