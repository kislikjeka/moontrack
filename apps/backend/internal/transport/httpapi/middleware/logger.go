package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// errCapture wraps chi's WrapResponseWriter to capture response body for error status codes.
type errCapture struct {
	chimiddleware.WrapResponseWriter
	buf        bytes.Buffer
	statusCode int
}

func (e *errCapture) WriteHeader(code int) {
	e.statusCode = code
	e.WrapResponseWriter.WriteHeader(code)
}

func (e *errCapture) Write(b []byte) (int, error) {
	if e.statusCode >= 400 {
		e.buf.Write(b)
	}
	return e.WrapResponseWriter.Write(b)
}

// extractErrorMessage tries to pull "error" field from a JSON response body.
func extractErrorMessage(body []byte) string {
	var obj struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &obj) == nil && obj.Error != "" {
		return obj.Error
	}
	return ""
}

// Logger returns a request logging middleware
func Logger(log *logger.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			ec := &errCapture{WrapResponseWriter: ww}
			start := time.Now()

			// Propagate chi's request ID into our typed context key
			reqID := chimiddleware.GetReqID(r.Context())
			if reqID != "" {
				ctx := context.WithValue(r.Context(), logger.RequestIDKey, reqID)
				r = r.WithContext(ctx)
			}

			defer func() {
				status := ww.Status()
				attrs := []any{
					"method", r.Method,
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
					"user_agent", r.UserAgent(),
					"status", status,
					"bytes", ww.BytesWritten(),
					"duration_ms", time.Since(start).Milliseconds(),
				}
				if reqID != "" {
					attrs = append(attrs, "request_id", reqID)
				}

				switch {
				case status >= 500:
					if msg := extractErrorMessage(ec.buf.Bytes()); msg != "" {
						attrs = append(attrs, "error", msg)
					}
					log.Error("HTTP request", attrs...)
				case status >= 400:
					if msg := extractErrorMessage(ec.buf.Bytes()); msg != "" {
						attrs = append(attrs, "error", msg)
					}
					log.Warn("HTTP request", attrs...)
				default:
					log.Info("HTTP request", attrs...)
				}
			}()

			next.ServeHTTP(ec, r)
		}
		return http.HandlerFunc(fn)
	}
}
