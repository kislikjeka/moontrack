package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/kislikjeka/moontrack/internal/shared/logger"
)

// Recovery returns a panic recovery middleware
func Recovery(log *logger.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("Panic recovered",
						"error", fmt.Sprintf("%v", rec),
						"path", r.URL.Path,
						"method", r.Method,
						"stack", string(debug.Stack()),
					)

					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	}
}
