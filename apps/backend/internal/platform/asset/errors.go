package asset

import (
	"errors"
	"fmt"
)

// Asset errors
var (
	ErrAssetNotFound       = errors.New("asset not found")
	ErrDuplicateAsset      = errors.New("asset already exists")
	ErrInvalidSymbol       = errors.New("invalid symbol")
	ErrInvalidName         = errors.New("invalid name")
	ErrCoinGeckoIDRequired = errors.New("coingecko_id is required")
	ErrInvalidDecimals     = errors.New("invalid decimals")
	ErrInvalidAssetType    = errors.New("invalid asset type")
)

// Price errors
var (
	ErrNilPrice          = errors.New("price cannot be nil")
	ErrNegativePrice     = errors.New("price cannot be negative")
	ErrNoPriceData       = errors.New("no price data available")
	ErrInvalidTimeRange  = errors.New("invalid time range")
	ErrTimeRangeTooLarge = errors.New("time range cannot exceed 1 year")
	ErrInvalidInterval   = errors.New("interval must be 1h, 1d, or 1w")
	ErrPriceUnavailable  = errors.New("price unavailable")
)

// AmbiguousSymbolError is returned when a symbol exists on multiple chains
// and no chain_id was specified to disambiguate
type AmbiguousSymbolError struct {
	Symbol   string
	ChainIDs []string
}

func (e *AmbiguousSymbolError) Error() string {
	return fmt.Sprintf("ambiguous symbol: %s exists on multiple chains: %v", e.Symbol, e.ChainIDs)
}

// Is implements errors.Is for AmbiguousSymbolError
func (e *AmbiguousSymbolError) Is(target error) bool {
	_, ok := target.(*AmbiguousSymbolError)
	return ok
}

// ErrAmbiguousSymbol is a sentinel for checking if an error is an AmbiguousSymbolError
var ErrAmbiguousSymbol = &AmbiguousSymbolError{}

// NewAmbiguousSymbolError creates a new AmbiguousSymbolError
func NewAmbiguousSymbolError(symbol string, chainIDs []string) *AmbiguousSymbolError {
	return &AmbiguousSymbolError{
		Symbol:   symbol,
		ChainIDs: chainIDs,
	}
}
