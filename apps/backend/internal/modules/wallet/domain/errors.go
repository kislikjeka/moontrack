package domain

import "errors"

var (
	// Validation errors
	ErrInvalidUserID      = errors.New("invalid user ID")
	ErrInvalidWalletID    = errors.New("invalid wallet ID")
	ErrMissingWalletName  = errors.New("wallet name is required")
	ErrWalletNameTooLong  = errors.New("wallet name exceeds 100 characters")
	ErrInvalidChainID     = errors.New("invalid or unsupported chain ID")
	ErrDuplicateWalletName = errors.New("wallet name already exists for this user")

	// Repository errors
	ErrWalletNotFound     = errors.New("wallet not found")
	ErrUnauthorizedAccess = errors.New("unauthorized wallet access")
)
