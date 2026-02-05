package adjustment

import "errors"

var (
	ErrInvalidWalletID   = errors.New("invalid wallet ID")
	ErrMissingAssetID    = errors.New("asset ID is required")
	ErrMissingNewBalance = errors.New("new balance is required")
	ErrNegativeBalance   = errors.New("balance cannot be negative")
	ErrNegativeUSDRate   = errors.New("USD rate cannot be negative")
	ErrFutureDate        = errors.New("occurred_at cannot be in the future")
)
