// apps/backend/internal/platform/price/errors.go
package price

import (
	"errors"
	"time"
)

var (
	// ErrNotFound — provider has no data for this asset. Counts as an attempt.
	ErrNotFound = errors.New("price: not found at provider")

	// ErrRateLimited — provider returned 429. Does NOT count as an attempt.
	//
	// Prefer returning a *RateLimitedError (which satisfies errors.Is(err, ErrRateLimited))
	// so callers can recover a Retry-After hint via errors.As.
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

// RateLimitedError is a typed wrapper around ErrRateLimited that carries the
// provider-supplied Retry-After hint. A zero RetryAfter means the provider did
// not supply a hint (or it was unparseable) — callers should fall back to a
// default delay.
//
// Usage:
//
//	var rle *price.RateLimitedError
//	if errors.As(err, &rle) { delay := rle.RetryAfter }
//	// or: errors.Is(err, price.ErrRateLimited)
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string { return ErrRateLimited.Error() }

// Is lets errors.Is(err, ErrRateLimited) return true.
func (e *RateLimitedError) Is(target error) bool { return target == ErrRateLimited }

// Unwrap so callers that check errors.Is against ErrRateLimited via wrapping also work.
func (e *RateLimitedError) Unwrap() error { return ErrRateLimited }
