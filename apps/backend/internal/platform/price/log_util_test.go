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

// TestSanitizeLogField_StripsUTF8LineSeparators verifies that the Unicode
// line separators that pass through a pure ASCII-byte filter (and would
// otherwise let a malicious provider forge log lines in JSON parsers that
// treat them as line breaks) are stripped.
func TestSanitizeLogField_StripsUTF8LineSeparators(t *testing.T) {
	cases := map[string]string{
		"U+2028 LINE SEPARATOR":      "malicious\u2028injected",
		"U+2029 PARAGRAPH SEPARATOR": "oops\u2029line2",
		"U+0085 NEL":                 "look\u0085line2",
		"U+007F DEL":                 "zap\u007fchar",
		"U+0099 C1 control":          "hi\u0099ctrl",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := sanitizeLogField(in)
			for _, r := range got {
				if r == 0x2028 || r == 0x2029 || r == 0x85 || r == 0x7F || (r >= 0x80 && r < 0xA0) {
					t.Fatalf("%s: forbidden rune U+%04X left in %q", name, r, got)
				}
			}
		})
	}
}

// TestSanitizeLogField_PreservesSafeUnicode verifies non-control Unicode
// (Cyrillic, CJK, emoji) is NOT stripped — only the genuinely dangerous
// line-separating and control runes are.
func TestSanitizeLogField_PreservesSafeUnicode(t *testing.T) {
	in := "приветmir世界🌍"
	got := sanitizeLogField(in)
	if got != in {
		t.Fatalf("expected %q preserved, got %q", in, got)
	}
}

// TestSanitizeLogField_Exported verifies the public alias is equivalent.
func TestSanitizeLogField_Exported(t *testing.T) {
	in := "ok\u2028bad"
	if sanitizeLogField(in) != SanitizeLogField(in) {
		t.Fatal("exported SanitizeLogField should equal sanitizeLogField")
	}
}
