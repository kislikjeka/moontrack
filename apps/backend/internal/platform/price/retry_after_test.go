// apps/backend/internal/platform/price/retry_after_test.go
package price

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter_DeltaSeconds(t *testing.T) {
	got := ParseRetryAfter("30")
	if got != 30*time.Second {
		t.Fatalf("expected 30s, got %v", got)
	}
}

func TestParseRetryAfter_ClampsLargeDeltaToTenMinutes(t *testing.T) {
	// 86400 seconds = 1 day; must clamp to 10m.
	got := ParseRetryAfter("86400")
	if got != 10*time.Minute {
		t.Fatalf("expected clamp to 10m, got %v", got)
	}
}

func TestParseRetryAfter_ClampsLargeHTTPDateToTenMinutes(t *testing.T) {
	// A date 1 hour in the future — must clamp to 10m.
	future := time.Now().UTC().Add(time.Hour).Format(http.TimeFormat)
	got := ParseRetryAfter(future)
	if got != 10*time.Minute {
		t.Fatalf("expected clamp to 10m, got %v", got)
	}
}

func TestParseRetryAfter_Empty(t *testing.T) {
	if got := ParseRetryAfter(""); got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}
}

func TestParseRetryAfter_Negative(t *testing.T) {
	if got := ParseRetryAfter("-5"); got != 0 {
		t.Fatalf("expected 0 for negative, got %v", got)
	}
}

func TestParseRetryAfter_Unparseable(t *testing.T) {
	if got := ParseRetryAfter("not a number"); got != 0 {
		t.Fatalf("expected 0 for garbage, got %v", got)
	}
}

func TestParseRetryAfter_PastDate(t *testing.T) {
	past := time.Now().UTC().Add(-time.Hour).Format(http.TimeFormat)
	if got := ParseRetryAfter(past); got != 0 {
		t.Fatalf("expected 0 for past date, got %v", got)
	}
}
