package defi

import "errors"

var (
	ErrInvalidWalletID = errors.New("invalid wallet ID")
	ErrInvalidChainID  = errors.New("invalid chain ID")
	ErrInvalidTxHash   = errors.New("invalid transaction hash")
	ErrNoTransfers     = errors.New("defi transaction must have at least one transfer")
	ErrNoInTransfers   = errors.New("defi claim must have at least one incoming transfer")
	ErrInvalidAmount   = errors.New("invalid amount: must be positive")
	ErrInvalidAssetID  = errors.New("invalid asset symbol")
	ErrInvalidDecimals = errors.New("invalid decimals: must be non-negative")
	ErrWalletNotFound  = errors.New("wallet not found")
	ErrUnauthorized    = errors.New("unauthorized: wallet does not belong to user")
)
