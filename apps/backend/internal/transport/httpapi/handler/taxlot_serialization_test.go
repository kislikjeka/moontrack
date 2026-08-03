package handler

import (
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kislikjeka/moontrack/internal/ledger"
)

// These tests assert on the SERIALIZED JSON rather than on struct fields.
// The #79 defect lived entirely in serialization: the domain carried a nil
// cost basis correctly, and the DTO turned it into "0.00" on the way out. A
// test that reads resp.AutoCostBasisPerUnit would have passed throughout.

// marshalLot renders a lot the way the endpoint does and decodes the result
// into a generic map, so a test can tell JSON null from the string "0.00" and
// from an absent key.
func marshalLot(t *testing.T, lot *ledger.TaxLot) map[string]any {
	t.Helper()

	raw, err := json.Marshal(toTaxLotResponse(lot, assetDescription{symbol: "ETH", decimals: 18}))
	if err != nil {
		t.Fatalf("marshal tax lot response: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal tax lot response: %v", err)
	}
	return decoded
}

// assertJSONNull fails unless the key is present AND holds null. Presence
// matters on its own: an omitted field reads as "this lot has no cost basis",
// while an explicit null says the price is not known yet.
func assertJSONNull(t *testing.T, obj map[string]any, key string) {
	t.Helper()

	value, ok := obj[key]
	if !ok {
		t.Fatalf("%q missing from JSON — an unknown value must ship as an explicit null, not be omitted", key)
	}
	if value != nil {
		t.Errorf("%q = %#v, want null — an unresolved price must not serialize as a number or \"0.00\"", key, value)
	}
}

func assertJSONString(t *testing.T, obj map[string]any, key, want string) {
	t.Helper()

	value, ok := obj[key]
	if !ok {
		t.Fatalf("%q missing from JSON, want %q", key, want)
	}
	if value != want {
		t.Errorf("%q = %#v, want %q", key, value, want)
	}
}

func pendingLot() *ledger.TaxLot {
	return &ledger.TaxLot{
		ID:                   uuid.New(),
		TransactionID:        uuid.New(),
		AccountID:            uuid.New(),
		Asset:                uuid.New(),
		QuantityAcquired:     big.NewInt(1_000_000_000_000_000_000),
		QuantityRemaining:    big.NewInt(1_000_000_000_000_000_000),
		AcquiredAt:           time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		AutoCostBasisPerUnit: nil, // no price at acquisition time
		AutoCostBasisSource:  ledger.CostBasisSource("market_price"),
		PriceStatus:          ledger.PriceStatusPending,
	}
}

// TestToTaxLotResponse_PendingLotSerializesNullNotZero is the regression this
// ticket exists for: the lot in the DB has auto_cost_basis_per_unit IS NULL and
// price_status='pending', and the API used to answer "0.00" with no status at
// all. Downstream that reads as "acquired for free", so the entire disposal
// counts as profit.
func TestToTaxLotResponse_PendingLotSerializesNullNotZero(t *testing.T) {
	decoded := marshalLot(t, pendingLot())

	assertJSONNull(t, decoded, "auto_cost_basis_per_unit")
	assertJSONNull(t, decoded, "effective_cost_basis_per_unit")
	assertJSONString(t, decoded, "price_status", "pending")
}

// TestToTaxLotResponse_ResolvedLotSerializesValue guards the other direction:
// preserving nil must not cost real prices their rendering.
func TestToTaxLotResponse_ResolvedLotSerializesValue(t *testing.T) {
	lot := pendingLot()
	lot.AutoCostBasisPerUnit = big.NewInt(4115226300) // $41.15
	lot.PriceStatus = ledger.PriceStatusResolved

	decoded := marshalLot(t, lot)

	assertJSONString(t, decoded, "auto_cost_basis_per_unit", "41.15")
	assertJSONString(t, decoded, "effective_cost_basis_per_unit", "41.15")
	assertJSONString(t, decoded, "price_status", "resolved")
}

// TestToTaxLotResponse_KnownZeroIsNotNull separates the two meanings that #79
// conflated. A genuinely zero cost basis (an airdrop, say) must still render as
// "0.00" — only an absent price becomes null.
func TestToTaxLotResponse_KnownZeroIsNotNull(t *testing.T) {
	lot := pendingLot()
	lot.AutoCostBasisPerUnit = big.NewInt(0)
	lot.PriceStatus = ledger.PriceStatusResolved

	decoded := marshalLot(t, lot)

	assertJSONString(t, decoded, "auto_cost_basis_per_unit", "0.00")
	assertJSONString(t, decoded, "effective_cost_basis_per_unit", "0.00")
}

// TestToTaxLotResponse_UnpriceableLotSerializesNull covers the terminal state:
// retries are exhausted, the price will never arrive, and zero would be a
// permanent lie rather than a temporary one.
func TestToTaxLotResponse_UnpriceableLotSerializesNull(t *testing.T) {
	lot := pendingLot()
	lot.PriceStatus = ledger.PriceStatusUnpriceable

	decoded := marshalLot(t, lot)

	assertJSONNull(t, decoded, "auto_cost_basis_per_unit")
	assertJSONString(t, decoded, "price_status", "unpriceable")
}

// TestToTaxLotResponse_OverrideResolvesPendingLot checks the priority rule
// survives: a user-supplied cost basis makes the effective value known even
// though the auto one never resolved.
func TestToTaxLotResponse_OverrideResolvesPendingLot(t *testing.T) {
	lot := pendingLot()
	lot.OverrideCostBasisPerUnit = big.NewInt(150000000) // $1.50

	decoded := marshalLot(t, lot)

	assertJSONNull(t, decoded, "auto_cost_basis_per_unit")
	assertJSONString(t, decoded, "override_cost_basis_per_unit", "1.50")
	assertJSONString(t, decoded, "effective_cost_basis_per_unit", "1.50")
}

// TestToTaxLotResponse_EmptyStatusDefaultsToResolved covers rows written before
// migration 000025 added price_status, which carry the resolved default.
func TestToTaxLotResponse_EmptyStatusDefaultsToResolved(t *testing.T) {
	lot := pendingLot()
	lot.AutoCostBasisPerUnit = big.NewInt(100000000)
	lot.PriceStatus = ""

	decoded := marshalLot(t, lot)

	assertJSONString(t, decoded, "price_status", "resolved")
}
