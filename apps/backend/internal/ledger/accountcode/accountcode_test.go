package accountcode_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
)

// walletID is a fixed UUID so the expected strings below can be written out in
// full rather than assembled, which is the point: these tests pin the literal
// shape of the protocol segment. They are meant to fail loudly when the shape
// changes, at which point the change is either deliberate — and the golden file
// in internal/module/accountcodegolden moves with it — or a bug.
var walletID = uuid.MustParse("11111111-1111-4111-8111-111111111111")

// usdc spells out the asset half of baseUSDC (declared in asset_test.go), so
// the expected codes below stay written out in full rather than assembled from
// the same expression that produced them.
const usdc = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

// onBase is the identity these tests hold fixed. The protocol segment is what
// varies here, so the chain/asset pair stays one value throughout — the same
// discipline the constructors now enforce on production callers.
func onBase() accountcode.Asset { return accountcode.OnChain("base", baseUSDC) }

// TestProtocolSegmentIsNormalised pins the slug rules for the protocol segment.
// Provider display names reach the constructor raw ("Fluid USD Coin"), and an
// absent name used to produce an empty segment — the two together split one
// lending position across two accounts on live data (#73).
func TestProtocolSegmentIsNormalised(t *testing.T) {
	const prefix = "collateral."
	const suffix = ".11111111-1111-4111-8111-111111111111.base." + usdc

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
			got := accountcode.Collateral(tt.proto, walletID, onBase())
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

	want := accountcode.Collateral(spellings[0], walletID, onBase())
	for _, s := range spellings[1:] {
		if got := accountcode.Collateral(s, walletID, onBase()); got != want {
			t.Errorf("spelling %q split the position:\n  got:  %s\n  want: %s", s, got, want)
		}
	}

	// Liability is scoped the same way and must agree.
	wantL := accountcode.Liability(spellings[0], walletID, onBase())
	for _, s := range spellings[1:] {
		if got := accountcode.Liability(s, walletID, onBase()); got != wantL {
			t.Errorf("liability spelling %q split the position:\n  got:  %s\n  want: %s", s, got, wantL)
		}
	}
}

// TestNormalisationIsIdempotent guards against a slug that changes when fed
// back through the constructor, which would reintroduce the split one sync later.
func TestNormalisationIsIdempotent(t *testing.T) {
	for _, proto := range []string{"Fluid USD Coin", "Aave v3.1", "", "Morpho Blue"} {
		once := accountcode.Collateral(proto, walletID, onBase())
		seg := strings.Split(once, ".")[1]
		twice := accountcode.Collateral(seg, walletID, onBase())
		if once != twice {
			t.Errorf("normalisation is not idempotent for %q:\n  once:  %s\n  twice: %s", proto, once, twice)
		}
	}
}
