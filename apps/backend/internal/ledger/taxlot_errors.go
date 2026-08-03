package ledger

import "errors"

// Tax lot errors
var (
	ErrInsufficientLots = errors.New("insufficient lots for disposal")
	ErrLotNotFound      = errors.New("tax lot not found")

	// ErrCannotClearOverrideOnPendingAuto is returned when ClearOverride is
	// called on a lot whose auto_cost_basis_per_unit is NULL. Clearing the
	// override would leave the lot with no effective cost basis and a
	// price_status that is not 'pending', so no backfill worker would ever
	// resolve it, causing downstream nil panics.
	ErrCannotClearOverrideOnPendingAuto = errors.New("cannot clear override: lot has no auto cost basis")

	// ErrNilResolvedPrice is returned when PriceResolvedHook is handed a nil
	// price. "Resolved" asserts that the price is known, so a nil one has
	// nothing to write and would mark lots resolved with no cost basis (#77).
	ErrNilResolvedPrice = errors.New("resolved price is nil")
)
