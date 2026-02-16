package swap

import "errors"

var (
	ErrInvalidWalletID  = errors.New("invalid wallet ID")
	ErrInvalidChainID   = errors.New("invalid chain ID")
	ErrInvalidTxHash    = errors.New("invalid transaction hash")
	ErrNoTransfers      = errors.New("swap must have at least one transfer in and one transfer out")
	ErrInvalidAmount    = errors.New("invalid amount: must be positive")
	ErrInvalidAssetID   = errors.New("invalid asset symbol")
	ErrWalletNotFound   = errors.New("wallet not found")
	ErrUnauthorized     = errors.New("unauthorized: wallet does not belong to user")
)
