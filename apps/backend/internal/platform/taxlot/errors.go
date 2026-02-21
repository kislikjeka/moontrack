package taxlot

import "errors"

var (
	ErrLotNotFound  = errors.New("tax lot not found")
	ErrLotNotOwned  = errors.New("tax lot does not belong to this user")
	ErrWalletNotOwned = errors.New("wallet does not belong to this user")
)
