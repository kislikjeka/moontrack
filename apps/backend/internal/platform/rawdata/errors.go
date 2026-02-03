package rawdata

import "errors"

var (
	// Income/Outcome errors
	ErrInvalidWalletID    = errors.New("invalid wallet ID")
	ErrInvalidAssetID     = errors.New("invalid asset ID")
	ErrInvalidAmount      = errors.New("invalid amount: must be positive")
	ErrInvalidUSDRate     = errors.New("invalid USD rate: must be positive")
	ErrOccurredAtInFuture = errors.New("occurred_at cannot be in the future")

	// Adjustment errors
	ErrMissingAssetID    = errors.New("asset ID is required")
	ErrMissingNewBalance = errors.New("new balance is required")
	ErrNegativeBalance   = errors.New("balance cannot be negative")
	ErrNegativeUSDRate   = errors.New("USD rate cannot be negative")
	ErrFutureDate        = errors.New("occurred_at cannot be in the future")
)
