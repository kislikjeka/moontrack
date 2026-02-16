package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// contextKey is a typed key for context values to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for request ID.
	RequestIDKey contextKey = "request_id"
	// UserIDKey is the context key for user ID (string representation).
	UserIDKey contextKey = "user_id"
)

// Logger is a structured logger wrapper around slog
type Logger struct {
	*slog.Logger
}

// New creates a new structured logger
func New(env string, output io.Writer) *Logger {
	return NewWithFormat(env, os.Getenv("LOG_FORMAT"), output)
}

// NewWithFormat creates a new structured logger with explicit format override.
func NewWithFormat(env, logFormat string, output io.Writer) *Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize timestamp format
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format(time.RFC3339))
				}
			}
			// Strip directory prefix from source, keep only filename:line
			if a.Key == slog.SourceKey {
				if src, ok := a.Value.Any().(*slog.Source); ok {
					file := src.File
					if idx := strings.LastIndex(file, "/"); idx >= 0 {
						file = file[idx+1:]
					}
					a.Value = slog.StringValue(fmt.Sprintf("%s:%d", file, src.Line))
				}
			}
			return a
		},
	}

	useJSON := logFormat == "json"

	if env == "production" {
		// Production always uses JSON at INFO level
		handler = slog.NewJSONHandler(output, opts)
	} else if useJSON {
		// LOG_FORMAT=json in development: JSON handler but keep DEBUG level
		opts.Level = slog.LevelDebug
		handler = slog.NewJSONHandler(output, opts)
	} else {
		// Development default: text handler with DEBUG level
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
	result := l
	if requestID := ctx.Value(RequestIDKey); requestID != nil {
		result = &Logger{Logger: result.With("request_id", requestID)}
	}
	if userID := ctx.Value(UserIDKey); userID != nil {
		result = &Logger{Logger: result.With("user_id", userID)}
	}
	return result
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

// WithDuration creates a new logger with a duration_ms field
func (l *Logger) WithDuration(d time.Duration) *Logger {
	return &Logger{
		Logger: l.With("duration_ms", d.Milliseconds()),
	}
}
