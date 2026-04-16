package logger

import "io"

// NewNoop returns a Logger that discards all output. Useful for tests.
func NewNoop() *Logger {
	return New("production", io.Discard)
}
