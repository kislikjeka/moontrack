package transfer

import "errors"

var (
	// Validation errors
	ErrInvalidWalletID       = errors.New("invalid wallet ID")
	ErrInvalidAssetID        = errors.New("invalid asset ID")
	ErrInvalidAmount         = errors.New("invalid amount: must be positive")
	ErrInvalidUSDRate        = errors.New("invalid USD rate: must be positive")
	ErrOccurredAtInFuture    = errors.New("occurred_at cannot be in the future")
	ErrInvalidTxHash         = errors.New("invalid transaction hash")
	ErrInvalidBlockNumber    = errors.New("invalid block number")
	ErrInvalidChainID        = errors.New("invalid chain ID")
	ErrMissingSourceWallet   = errors.New("source wallet ID is required for internal transfer")
	ErrMissingDestWallet     = errors.New("destination wallet ID is required for internal transfer")
	ErrSameWalletTransfer    = errors.New("source and destination wallets cannot be the same")

	// Authorization errors
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrUnauthorized          = errors.New("unauthorized: wallet does not belong to user")

	// Duplicate detection
	ErrDuplicateTransfer     = errors.New("transfer already recorded")
)
