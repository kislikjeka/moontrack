package ledger

import "errors"

// Tax lot errors
var (
	ErrInsufficientLots = errors.New("insufficient lots for disposal")
	ErrLotNotFound      = errors.New("tax lot not found")
)
