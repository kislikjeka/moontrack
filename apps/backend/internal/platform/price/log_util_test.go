// apps/backend/internal/platform/price/log_util_test.go
package price

import (
	"strings"
	"testing"
)

func TestSanitizeLogField_StripsNewlines(t *testing.T) {
	in := "hello\nworld\r\nfake_field=injected"
	got := sanitizeLogField(in)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("expected newlines stripped, got %q", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatalf("expected content preserved, got %q", got)
	}
}

func TestSanitizeLogField_StripsControlChars(t *testing.T) {
	in := "foo\x00bar\x01baz\tqux"
	got := sanitizeLogField(in)
	for i := 0; i < len(got); i++ {
		if got[i] < 0x20 {
			t.Fatalf("found control byte 0x%02x in output %q", got[i], got)
		}
	}
}

func TestSanitizeLogField_CapsLength(t *testing.T) {
	in := strings.Repeat("a", 1000)
	got := sanitizeLogField(in)
	if len(got) != 500 {
		t.Fatalf("expected length 500, got %d", len(got))
	}
}

func TestSanitizeLogField_PreservesPrintable(t *testing.T) {
	in := "normal error: status 500, body=xyz"
	got := sanitizeLogField(in)
	if got != in {
		t.Fatalf("expected %q, got %q", in, got)
	}
}

func TestSanitizeLogField_PreservesUTF8(t *testing.T) {
	in := "привет мир"
	got := sanitizeLogField(in)
	if got != in {
		t.Fatalf("expected utf-8 preserved: %q, got %q", in, got)
	}
}

func TestSanitizeLogField_Empty(t *testing.T) {
	if sanitizeLogField("") != "" {
		t.Fatal("expected empty input to pass through")
	}
}
