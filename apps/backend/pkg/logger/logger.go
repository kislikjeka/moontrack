package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"
)

// Logger is a structured logger wrapper around slog
type Logger struct {
	*slog.Logger
}

// New creates a new structured logger
func New(env string, output io.Writer) *Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize timestamp format
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format(time.RFC3339))
				}
			}
			return a
		},
	}

	// Use JSON handler for production, text handler for development
	if env == "production" {
		handler = slog.NewJSONHandler(output, opts)
	} else {
		// Enable debug level in development
		opts.Level = slog.LevelDebug
		handler = slog.NewTextHandler(output, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
	}
}

// NewDefault creates a new logger with default settings (stdout)
func NewDefault(env string) *Logger {
	return New(env, os.Stdout)
}

// WithContext adds context fields to the logger
func (l *Logger) WithContext(ctx context.Context) *Logger {
	// Extract request ID or other context values if present
	if requestID := ctx.Value("request_id"); requestID != nil {
		return &Logger{
			Logger: l.With("request_id", requestID),
		}
	}
	return l
}

// WithFields creates a new logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return &Logger{
		Logger: l.With(args...),
	}
}

// WithField creates a new logger with an additional field
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{
		Logger: l.With(key, value),
	}
}

// WithError creates a new logger with an error field
func (l *Logger) WithError(err error) *Logger {
	return &Logger{
		Logger: l.With("error", err.Error()),
	}
}
