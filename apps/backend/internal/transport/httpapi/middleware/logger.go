package middleware

import (
	"context"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/kislikjeka/moontrack/pkg/logger"
)

// Logger returns a request logging middleware
func Logger(log *logger.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
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
					log.Error("HTTP request", attrs...)
				case status >= 400:
					log.Warn("HTTP request", attrs...)
				default:
					log.Info("HTTP request", attrs...)
				}
			}()

			next.ServeHTTP(ww, r)
		}
		return http.HandlerFunc(fn)
	}
}
