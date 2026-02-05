package wallet

import "errors"

var (
	// Validation errors
	ErrInvalidUserID       = errors.New("invalid user ID")
	ErrInvalidWalletID     = errors.New("invalid wallet ID")
	ErrMissingWalletName   = errors.New("wallet name is required")
	ErrWalletNameTooLong   = errors.New("wallet name exceeds 100 characters")
	ErrInvalidChainID      = errors.New("invalid or unsupported chain ID")
	ErrDuplicateWalletName = errors.New("wallet name already exists for this user")

	// Address validation errors
	ErrMissingAddress     = errors.New("wallet address is required")
	ErrInvalidAddress     = errors.New("invalid EVM address format (must be 0x followed by 40 hex characters)")
	ErrInvalidChecksum    = errors.New("invalid EVM address checksum")
	ErrDuplicateAddress   = errors.New("wallet address already exists for this chain and user")

	// Repository errors
	ErrWalletNotFound     = errors.New("wallet not found")
	ErrUnauthorizedAccess = errors.New("unauthorized wallet access")

	// Sync errors
	ErrSyncInProgress     = errors.New("wallet sync already in progress")
	ErrSyncNotSupported   = errors.New("wallet sync not supported for this chain")
)
