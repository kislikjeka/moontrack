// apps/backend/internal/platform/price/retry_after.go
package price

import (
	"net/http"
	"strconv"
	"time"
)

// maxRetryAfter bounds the Retry-After hint we will honor from a 3rd-party
// provider. A hostile or buggy upstream can return absurd values like
// "86400" (one day) or a date years in the future, which would stall the
// worker long after the rate-limit window has actually passed. We clamp to
// 10 minutes as a defensive upper bound.
const maxRetryAfter = 10 * time.Minute

// ParseRetryAfter parses an HTTP Retry-After header value. It accepts either
// delta-seconds (e.g. "30") or an HTTP-date (e.g. "Wed, 21 Oct 2025 07:28:00 GMT").
// Returns 0 if the value is empty, unparseable, negative, or in the past.
// The returned duration is clamped to maxRetryAfter.
func ParseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	// delta-seconds
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		d := time.Duration(secs) * time.Second
		if d > maxRetryAfter {
			d = maxRetryAfter
		}
		return d
	}
	// HTTP-date (RFC 7231). http.ParseTime accepts RFC1123, RFC850, ANSI-C asctime.
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0
		}
		if d > maxRetryAfter {
			d = maxRetryAfter
		}
		return d
	}
	return 0
}
