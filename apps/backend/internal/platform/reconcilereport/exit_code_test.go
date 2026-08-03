package reconcilereport

import "testing"

// TestExitCodesAreDistinguishable pins all three codes on constructed states, so
// the contract is verified rather than observed once on one database.
func TestExitCodesAreDistinguishable(t *testing.T) {
	t.Run("0 when the red category is empty", func(t *testing.T) {
		if got := Build(baselineInput(t)).ExitCode(); got != 0 {
			t.Fatalf("want 0, got %d", got)
		}
	})
	t.Run("1 when a row is red", func(t *testing.T) {
		in := baselineInput(t)
		in.Positions = append(in.Positions, Position{
			Key:    keyFor("base", "0x9999999999999999999999999999999999999999"),
			Symbol: "ORPHAN", Decimals: 18, Quantity: bi("1000"),
		})
		if got := Build(in).ExitCode(); got != 1 {
			t.Fatalf("want 1, got %d", got)
		}
	})
	t.Run("2 when a check could not run, even with red rows present", func(t *testing.T) {
		in := baselineInput(t)
		in.Ledger.Accounts[0].MaterializedBalance = bi("1") // a finding on check 2
		in.PositionsAvailable = false
		in.PositionsUnavailableReason = "provider down"
		if got := Build(in).ExitCode(); got != 2 {
			t.Fatalf("want 2 — 'was not checked' outranks 'did not add up', got %d", got)
		}
	})
}
