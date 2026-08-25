package accountcode_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger/accountcode"
)

// Two registry UUIDs standing for the same token on two chains — the exact
// configuration that produced #70. Written out in full because the expected
// strings below are written out in full too.
var (
	ethUSDC  = uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	baseUSDC = uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
)

// TestCodeShapes is the golden set for the package: one representative input
// per namespace, pinned to the literal string it must produce.
//
// It began as a parity check between the identity-taking constructors and the
// string-taking ones, which is what made the migration in #83/#84 mechanical —
// a call site could be rewritten without touching the accounts it addressed.
// The string form is gone as of #85, so what survives is the half that still
// carries weight: the literal `want`, spelled out rather than derived, so a
// shape change has to be made twice and therefore deliberately.
func TestCodeShapes(t *testing.T) {
	const (
		ethChain  = "ethereum"
		baseChain = "base"
		eth       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		base      = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		wallet    = "11111111-1111-4111-8111-111111111111"
	)

	ethAsset := accountcode.OnChain(ethChain, ethUSDC)
	baseAsset := accountcode.OnChain(baseChain, baseUSDC)

	tests := []struct {
		name     string
		identity string
		want     string
	}{
		{
			"wallet",
			accountcode.Wallet(walletID, ethAsset),
			"wallet." + wallet + ".ethereum." + eth,
		},
		{
			"income",
			accountcode.Income(ethAsset),
			"income.ethereum." + eth,
		},
		{
			"income genesis",
			accountcode.IncomeGenesis(baseAsset),
			"income.genesis.base." + base,
		},
		{
			"income lp",
			accountcode.IncomeLp(baseAsset),
			"income.lp.base." + base,
		},
		{
			"income defi",
			accountcode.IncomeDefi(ethAsset),
			"income.defi.ethereum." + eth,
		},
		{
			"income lending",
			accountcode.IncomeLending(baseAsset),
			"income.lending.base." + base,
		},
		{
			"expense",
			accountcode.Expense(ethAsset),
			"expense.ethereum." + eth,
		},
		{
			"gas",
			accountcode.Gas(ethAsset),
			"gas.ethereum." + eth,
		},
		{
			"clearing",
			accountcode.Clearing(baseAsset),
			"clearing.base." + base,
		},
		{
			"collateral",
			accountcode.Collateral("aave-v3", walletID, baseAsset),
			"collateral.aave-v3." + wallet + ".base." + base,
		},
		{
			"liability",
			accountcode.Liability("aave-v3", walletID, baseAsset),
			"liability.aave-v3." + wallet + ".base." + base,
		},
	}

	if len(tests) != 11 {
		t.Fatalf("the package has 11 namespaces; the golden set covers %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.identity != tt.want {
				t.Errorf("account code shape changed:\n  got:  %s\n  want: %s", tt.identity, tt.want)
			}
		})
	}
}

// TestProtocolStaysASeparateArgument pins that only the chain/asset pair folded
// into the identity. The protocol is a second scope on top of the asset, not
// part of it — the same asset supplied to two protocols is two positions — and
// it is still normalised by the same slug rules (#73).
func TestProtocolStaysASeparateArgument(t *testing.T) {
	asset := accountcode.OnChain("base", baseUSDC)

	aave := accountcode.Collateral("Aave v3", walletID, asset)
	morpho := accountcode.Collateral("Morpho Blue", walletID, asset)

	if aave == morpho {
		t.Fatalf("one asset in two protocols collapsed onto one account: %s", aave)
	}
	if seg := strings.Split(aave, ".")[1]; seg != "aave-v3" {
		t.Errorf("protocol segment not normalised: got %q, want %q", seg, "aave-v3")
	}
	if seg := strings.Split(accountcode.Liability("", walletID, asset), ".")[1]; seg != accountcode.UnknownProtocol {
		t.Errorf("absent protocol lost its sentinel: got %q, want %q", seg, accountcode.UnknownProtocol)
	}
}

// TestAssetIsBuiltOnlyByOnChain is the inexpressibility check the ticket asks
// for: a mismatched pair — one asset's chain with another asset's UUID — must
// not be constructible outside this package.
//
// Go has no way to assert "this does not compile" from inside a test, so the
// property is checked at its source instead: every field of Asset is
// unexported. That is precisely what rules the mismatch out. A composite
// literal outside the package cannot name the fields, and there is no setter
// or exported field to overwrite one half after the fact, so the only way to
// obtain a populated Asset is OnChain — which demands both halves at once.
// Exporting a field later would make this fail, which is the point.
func TestAssetIsBuiltOnlyByOnChain(t *testing.T) {
	typ := reflect.TypeOf(accountcode.OnChain("ethereum", ethUSDC))

	if typ.Kind() != reflect.Struct {
		t.Fatalf("Asset must be a struct to keep its parts unreachable, got %s", typ.Kind())
	}
	if typ.NumField() == 0 {
		t.Fatal("Asset carries no fields; it cannot be holding both halves")
	}

	for i := range typ.NumField() {
		if f := typ.Field(i); f.IsExported() {
			t.Errorf("field %s is exported: a caller can now build a mismatched pair by hand", f.Name)
		}
	}

	// No exported method may hand back a half either — that would let a caller
	// take one asset's chain apart and pair it with another asset's UUID
	// through OnChain, reopening #70 through the front door.
	for i := range typ.NumMethod() {
		t.Errorf("Asset exposes method %s; the pair must stay opaque", typ.Method(i).Name)
	}
}

// TestIdentityCarriesTheSamePairToEveryNamespace states the invariant the type
// buys, as behaviour rather than as structure: one Asset value produces the
// same trailing chain/asset pair everywhere it is used. In #70 the two halves
// arrived from different expressions at neighbouring call sites and drifted;
// with one value there is nothing left to drift.
func TestIdentityCarriesTheSamePairToEveryNamespace(t *testing.T) {
	asset := accountcode.OnChain("base", baseUSDC)
	const wantTail = "base.bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	codes := map[string]string{
		"wallet":         accountcode.Wallet(walletID, asset),
		"income":         accountcode.Income(asset),
		"income genesis": accountcode.IncomeGenesis(asset),
		"income lp":      accountcode.IncomeLp(asset),
		"income defi":    accountcode.IncomeDefi(asset),
		"income lending": accountcode.IncomeLending(asset),
		"expense":        accountcode.Expense(asset),
		"gas":            accountcode.Gas(asset),
		"clearing":       accountcode.Clearing(asset),
		"collateral":     accountcode.Collateral("aave-v3", walletID, asset),
		"liability":      accountcode.Liability("aave-v3", walletID, asset),
	}

	for name, code := range codes {
		if !strings.HasSuffix(code, "."+wantTail) {
			t.Errorf("%s did not end in the identity it was given:\n  got:  %s\n  want suffix: .%s", name, code, wantTail)
		}
	}
}

// TestSameTokenOnTwoChainsGetsTwoAccounts is #70 stated as a property: the same
// token bridged to another chain is another asset, and must not share an
// account with its source-chain twin.
func TestSameTokenOnTwoChainsGetsTwoAccounts(t *testing.T) {
	source := accountcode.Wallet(walletID, accountcode.OnChain("ethereum", ethUSDC))
	dest := accountcode.Wallet(walletID, accountcode.OnChain("base", baseUSDC))

	if source == dest {
		t.Fatalf("both legs of a bridge landed on one account: %s", source)
	}
	if strings.HasSuffix(source, "."+baseUSDC.String()) || strings.HasSuffix(dest, "."+ethUSDC.String()) {
		t.Errorf("a chain segment was paired with the other asset's UUID:\n  source: %s\n  dest:   %s", source, dest)
	}
}

// TestCaseIsPreservedByIdentityForm guards the same property as the string
// form: chain and asset travel through verbatim. Normalising either here would
// silently split one position across two accounts.
func TestCaseIsPreservedByIdentityForm(t *testing.T) {
	got := accountcode.Wallet(walletID, accountcode.OnChain("Arbitrum-One", baseUSDC))
	want := "wallet.11111111-1111-4111-8111-111111111111.Arbitrum-One.bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	if got != want {
		t.Errorf("chain segment was not preserved:\n  got:  %s\n  want: %s", got, want)
	}
}
