// apps/backend/internal/platform/price/log_util.go
package price

import (
	"strings"
	"unicode/utf8"
)

// SanitizeLogField (exported form of sanitizeLogField) strips control
// characters and line-separating runes and caps length so provider-supplied
// strings cannot forge log records when logs are parsed via `| json`
// downstream. It is safe to call from other packages that need the same
// trust-boundary treatment (see: infra/postgres/asset_repo.go).
//
// Log-forge vector: a provider error message can contain newlines, backslashes,
// or other control runes. If we emit them verbatim into a structured log line
// (k=v or JSON), a malicious payload can synthesize fake fields or fake records
// in the downstream parser.
//
// Rules:
//   - Cap length at 500 bytes.
//   - Walk runes (utf8.DecodeRuneInString) and replace with ASCII space any
//     rune where:
//   - r < 0x20 (C0 controls incl. \r, \n, \t)
//   - r == 0x7F (DEL)
//   - r == 0x85 (NEL — Next Line)
//   - r == 0x2028 (LINE SEPARATOR)
//   - r == 0x2029 (PARAGRAPH SEPARATOR)
//   - 0x80 <= r < 0xA0 (C1 controls)
//   - RuneError (invalid UTF-8 byte) is replaced with space too.
//   - Other runes are preserved.
func SanitizeLogField(s string) string {
	return sanitizeLogFieldWithCap(s, 500)
}

// sanitizeLogField is the internal package-private alias kept so existing
// callers inside the price package don't need to change.
func sanitizeLogField(s string) string {
	return SanitizeLogField(s)
}

// sanitizeLogFieldWithCap exposes a configurable cap for callers that want a
// different trust-boundary length (e.g. asset_repo.go's provider-field
// sanitizer historically used 32/128-byte caps).
func sanitizeLogFieldWithCap(s string, maxLen int) string {
	if len(s) > maxLen {
		// Trim to maxLen bytes at the nearest rune boundary so we never leave
		// a dangling partial UTF-8 sequence. If the cut falls inside a
		// multi-byte rune, walk back up to 3 bytes to the start of the rune
		// and truncate there.
		cut := maxLen
		for cut > 0 && cut < len(s) && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size <= 1:
			// Invalid UTF-8 byte.
			b.WriteByte(' ')
		case r < 0x20: // C0 controls (incl. \t, \n, \r)
			b.WriteByte(' ')
		case r == 0x7F: // DEL
			b.WriteByte(' ')
		case r == 0x85: // NEL (Next Line)
			b.WriteByte(' ')
		case r >= 0x80 && r < 0xA0: // C1 controls
			b.WriteByte(' ')
		case r == 0x2028 || r == 0x2029: // LINE SEPARATOR / PARAGRAPH SEPARATOR
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}
