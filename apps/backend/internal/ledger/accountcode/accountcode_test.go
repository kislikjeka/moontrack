package accountcode_test

import (
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
