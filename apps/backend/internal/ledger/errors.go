package ledger

import "errors"

// Account errors
var (
	ErrInvalidAccountCode                 = errors.New("invalid account code")
	ErrInvalidAssetID                     = errors.New("invalid asset ID")
	ErrInvalidAccountType                 = errors.New("invalid account type")
	ErrWalletAccountRequiresWalletID      = errors.New("wallet account requires wallet ID")
	ErrNonWalletAccountCannotHaveWalletID = errors.New("non-wallet account cannot have wallet ID")
)

// Entry errors
var (
	ErrInvalidEntryType   = errors.New("invalid entry type")
	ErrInvalidDebitCredit = errors.New("invalid debit/credit value")
	ErrNegativeAmount     = errors.New("amount cannot be negative")
	ErrNegativeUSDRate    = errors.New("USD rate cannot be negative")
	ErrNegativeUSDValue   = errors.New("USD value cannot be negative")
)

// Transaction errors
var (
	ErrInvalidTransactionType   = errors.New("invalid transaction type")
	ErrInvalidTransactionStatus = errors.New("invalid transaction status")
	ErrTransactionNotBalanced   = errors.New("transaction debits and credits do not balance")
	ErrOccurredAtInFuture       = errors.New("occurred_at cannot be in the future")
	ErrOccurredAtAfterRecorded  = errors.New("occurred_at cannot be after recorded_at")
)

// Balance errors
var (
	ErrNegativeBalance = errors.New("balance cannot be negative")
)
