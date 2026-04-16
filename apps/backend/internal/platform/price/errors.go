// apps/backend/internal/platform/price/errors.go
package price

import "errors"

var (
	// ErrNotFound — provider has no data for this asset. Counts as an attempt.
	ErrNotFound = errors.New("price: not found at provider")

	// ErrRateLimited — provider returned 429. Does NOT count as an attempt.
	ErrRateLimited = errors.New("price: provider rate limited")

	// ErrTransient — 5xx or network error. Does NOT count as an attempt.
	ErrTransient = errors.New("price: transient provider error")

	// ErrLowConfidence — provider returned data below confidence threshold.
	// Treated like NotFound (counts as attempt).
	ErrLowConfidence = errors.New("price: provider confidence below threshold")

	// ErrUnsupportedChain — provider does not cover this chain. Counts as attempt.
	ErrUnsupportedChain = errors.New("price: provider does not support chain")

	// ErrPending — resolver found no resolved price; job is still pending.
	ErrPending = errors.New("price: resolution pending")

	// ErrUnpriceable — lot exhausted all attempts; no price available anywhere.
	ErrUnpriceable = errors.New("price: unpriceable")
)
