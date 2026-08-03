package accountcode_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
)

// walletID is a fixed UUID so the expected strings below can be written out in
// full rather than assembled, which is the point: these tests pin the literal
// shape of every namespace. They are meant to fail loudly when the shape
// changes, at which point the change is either deliberate — and the golden file
// in internal/module/accountcodegolden moves with it — or a bug.
var walletID = uuid.MustParse("11111111-1111-4111-8111-111111111111")

func TestCodeShapes(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			"wallet",
			accountcode.WalletCode(walletID, "ethereum", "ETH"),
			"wallet.11111111-1111-4111-8111-111111111111.ethereum.ETH",
		},
		{
			"income",
			accountcode.IncomeCode("ethereum", "ETH"),
			"income.ethereum.ETH",
		},
		{
			"income genesis",
			accountcode.IncomeGenesisCode("bitcoin", "BTC"),
			"income.genesis.bitcoin.BTC",
		},
		{
			"income lp",
			accountcode.IncomeLpCode("base", "USDC"),
			"income.lp.base.USDC",
		},
		{
			"income defi",
			accountcode.IncomeDefiCode("ethereum", "CRV"),
			"income.defi.ethereum.CRV",
		},
		{
			"income lending",
			accountcode.IncomeLendingCode("arbitrum", "AAVE"),
			"income.lending.arbitrum.AAVE",
		},
		{
			"expense",
			accountcode.ExpenseCode("ethereum", "USDC"),
			"expense.ethereum.USDC",
		},
		{
			"gas",
			accountcode.GasCode("ethereum", "ETH"),
			"gas.ethereum.ETH",
		},
		{
			"clearing",
			accountcode.ClearingCode("ethereum", "USDC"),
			"clearing.ethereum.USDC",
		},
		{
			"collateral",
			accountcode.CollateralCode("aave-v3", walletID, "arbitrum", "USDC"),
			"collateral.aave-v3.11111111-1111-4111-8111-111111111111.arbitrum.USDC",
		},
		{
			"liability",
			accountcode.LiabilityCode("aave-v3", walletID, "arbitrum", "DAI"),
			"liability.aave-v3.11111111-1111-4111-8111-111111111111.arbitrum.DAI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("account code shape changed:\n  got:  %s\n  want: %s", tt.got, tt.want)
			}
		})
	}
}

// TestProtocolSegmentIsNormalised pins the slug rules for the protocol segment.
// Provider display names reach the constructor raw ("Fluid USD Coin"), and an
// absent name used to produce an empty segment — the two together split one
// lending position across two accounts on live data (#73).
func TestProtocolSegmentIsNormalised(t *testing.T) {
	const prefix = "collateral."
	const suffix = ".11111111-1111-4111-8111-111111111111.base.USDC"

	tests := []struct {
		name  string
		proto string
		want  string
	}{
		{"already a slug", "aave-v3", "aave-v3"},
		{"display name with spaces", "Fluid USD Coin", "fluid-usd-coin"},
		{"empty is an explicit sentinel", "", "unknown"},
		{"whitespace only is a sentinel too", "   ", "unknown"},
		{"punctuation only is a sentinel too", "...", "unknown"},
		{"dots do not add a segment", "Aave v3.1", "aave-v3-1"},
		{"runs of separators collapse", "Fluid  USD / Coin", "fluid-usd-coin"},
		{"leading and trailing separators are trimmed", "  Morpho Blue  ", "morpho-blue"},
		{"non-ascii degrades to separators", "Aave×v3", "aave-v3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := accountcode.CollateralCode(tt.proto, walletID, "base", "USDC")
			want := prefix + tt.want + suffix
			if got != want {
				t.Errorf("protocol segment not normalised:\n  got:  %s\n  want: %s", got, want)
			}

			// The arity of the code is the contract downstream parsers rely on.
			if n := len(strings.Split(got, ".")); n != 5 {
				t.Errorf("code must have exactly 5 segments, got %d: %s", n, got)
			}
		})
	}
}

// TestProtocolSpellingsShareOneAccount is the defect from #73 stated directly:
// the same position, named differently by the provider between two syncs, must
// resolve to one account code. Before normalisation the supply landed on one
// spelling and the withdraw looked for another, hit a zero balance, and failed
// the transaction.
func TestProtocolSpellingsShareOneAccount(t *testing.T) {
	spellings := []string{
		"Fluid USD Coin",
		"fluid usd coin",
		"Fluid  USD  Coin",
		"fluid-usd-coin",
	}

	want := accountcode.CollateralCode(spellings[0], walletID, "base", "USDC")
	for _, s := range spellings[1:] {
		if got := accountcode.CollateralCode(s, walletID, "base", "USDC"); got != want {
			t.Errorf("spelling %q split the position:\n  got:  %s\n  want: %s", s, got, want)
		}
	}

	// Liability is scoped the same way and must agree.
	wantL := accountcode.LiabilityCode(spellings[0], walletID, "base", "USDC")
	for _, s := range spellings[1:] {
		if got := accountcode.LiabilityCode(s, walletID, "base", "USDC"); got != wantL {
			t.Errorf("liability spelling %q split the position:\n  got:  %s\n  want: %s", s, got, wantL)
		}
	}
}

// TestNormalisationIsIdempotent guards against a slug that changes when fed
// back through the constructor, which would reintroduce the split one sync later.
func TestNormalisationIsIdempotent(t *testing.T) {
	for _, proto := range []string{"Fluid USD Coin", "Aave v3.1", "", "Morpho Blue"} {
		once := accountcode.CollateralCode(proto, walletID, "base", "USDC")
		seg := strings.Split(once, ".")[1]
		twice := accountcode.CollateralCode(seg, walletID, "base", "USDC")
		if once != twice {
			t.Errorf("normalisation is not idempotent for %q:\n  once:  %s\n  twice: %s", proto, once, twice)
		}
	}
}

// TestCaseIsPreserved guards a property the ledger depends on downstream:
// account codes are compared with exact equality, so the constructor must pass
// chain and asset through verbatim. Normalising case here would silently split
// one position across two accounts.
func TestCaseIsPreserved(t *testing.T) {
	got := accountcode.WalletCode(walletID, "ethereum", "crvUSDC")
	want := "wallet.11111111-1111-4111-8111-111111111111.ethereum.crvUSDC"

	if got != want {
		t.Errorf("mixed-case asset symbol was not preserved:\n  got:  %s\n  want: %s", got, want)
	}
}
