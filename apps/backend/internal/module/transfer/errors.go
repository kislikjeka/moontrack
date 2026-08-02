package transfer

import "errors"

var (
	// Validation errors
	ErrInvalidWalletID     = errors.New("invalid wallet ID")
	ErrInvalidAssetID      = errors.New("invalid asset ID")
	ErrInvalidAmount       = errors.New("invalid amount: must be positive")
	ErrInvalidUSDRate      = errors.New("invalid USD rate: must be positive")
	ErrOccurredAtInFuture  = errors.New("occurred_at cannot be in the future")
	ErrInvalidTxHash       = errors.New("invalid transaction hash")
	ErrInvalidBlockNumber  = errors.New("invalid block number")
	ErrInvalidChainID      = errors.New("invalid chain ID")
	ErrMissingSourceWallet = errors.New("source wallet ID is required for internal transfer")
	ErrMissingDestWallet   = errors.New("destination wallet ID is required for internal transfer")
	ErrSameWalletTransfer  = errors.New("source and destination wallets cannot be the same")
	// ErrMissingNativeAsset fires when a gas fee is present but the chain's
	// native asset was not resolved. Before #59 this defaulted to "ETH", so a
	// fee paid in MATIC or BNB was charged to an ETH gas account and decremented
	// an ETH balance the wallet never spent. Failing is the only honest answer:
	// there is no chain-independent guess that is right.
	ErrMissingNativeAsset = errors.New("native asset ID is required to book a gas fee")

	// Authorization errors
	ErrWalletNotFound = errors.New("wallet not found")
	ErrUnauthorized   = errors.New("unauthorized: wallet does not belong to user")

	// Duplicate detection
	ErrDuplicateTransfer = errors.New("transfer already recorded")
)
