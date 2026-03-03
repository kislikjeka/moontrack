package lending

import "errors"

var (
	ErrInvalidWalletID = errors.New("invalid wallet ID")
	ErrInvalidChainID  = errors.New("invalid chain ID")
	ErrInvalidTxHash   = errors.New("invalid transaction hash")
	ErrInvalidAsset    = errors.New("invalid asset: must not be empty")
	ErrInvalidAmount   = errors.New("invalid amount: must be positive")
	ErrInvalidDecimals = errors.New("invalid decimals: must be positive")
	ErrWalletNotFound  = errors.New("wallet not found")
	ErrUnauthorized    = errors.New("unauthorized: wallet does not belong to user")
)
