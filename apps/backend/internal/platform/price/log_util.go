// apps/backend/internal/platform/price/log_util.go
package price

// sanitizeLogField strips control characters and caps length so provider-supplied
// strings cannot forge log records when logs are parsed via `| json` downstream.
//
// Log-forge vector: a provider error message can contain newlines, backslashes,
// or other control bytes. If we emit them verbatim into a structured log line
// (k=v or JSON), a malicious payload can synthesize fake fields or fake records
// in the downstream parser.
//
// Rules:
//   - Cap length at 500 bytes.
//   - Replace bytes < 0x20 (incl. \r, \n, \t) with space.
//   - Leave 0x20..0x7E intact; leave UTF-8 continuation bytes (>= 0x80) intact.
//     Note: we operate byte-wise, not rune-wise, because the goal is purely to
//     neutralize ASCII control bytes. This is safe for any UTF-8 input: multi-byte
//     UTF-8 continuation bytes are always >= 0x80 and will never be replaced.
func sanitizeLogField(s string) string {
	const maxLen = 500
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\r' || c == '\n' || c < 0x20 {
			b = append(b, ' ')
		} else {
			b = append(b, c)
		}
	}
	return string(b)
}
