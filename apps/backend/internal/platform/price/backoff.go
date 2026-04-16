// apps/backend/internal/platform/price/backoff.go
package price

import "time"

// MaxAttempts — after this many attempts with ErrNotFound/ErrLowConfidence/ErrUnsupportedChain,
// a lot is marked unpriceable.
const MaxAttempts = 11

// BackoffDelay returns how long to wait before the next attempt.
// attempt is 1-indexed (attempt=1 is the first retry after the initial miss).
func BackoffDelay(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 15 * time.Minute
	case attempt == 2:
		return 1 * time.Hour
	case attempt == 3:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// IsTerminalAttempt returns true if this attempt number should mark the lot unpriceable.
func IsTerminalAttempt(attempt int) bool {
	return attempt >= MaxAttempts
}
