package domain

import "errors"

var (
	// ErrInvalidWalletID is returned when wallet ID is invalid
	ErrInvalidWalletID = errors.New("invalid wallet ID")

	// ErrInvalidAssetID is returned when asset ID is invalid
	ErrInvalidAssetID = errors.New("invalid asset ID")

	// ErrInvalidAmount is returned when amount is invalid (zero or negative)
	ErrInvalidAmount = errors.New("invalid amount: must be positive")

	// ErrInvalidUSDRate is returned when USD rate is invalid
	ErrInvalidUSDRate = errors.New("invalid USD rate: must be positive")

	// ErrOccurredAtInFuture is returned when occurred_at is in the future
	ErrOccurredAtInFuture = errors.New("occurred_at cannot be in the future")

	// ErrInsufficientBalance is returned when wallet balance is insufficient for outcome transaction
	ErrInsufficientBalance = errors.New("insufficient balance")

	// ErrWalletNotFound is returned when wallet is not found
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrUnauthorized is returned when user doesn't own the wallet
	ErrUnauthorized = errors.New("unauthorized: wallet does not belong to user")

	// ErrPriceNotAvailable is returned when price cannot be fetched
	ErrPriceNotAvailable = errors.New("price not available")
)
