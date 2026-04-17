package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover catches panics in any inner middleware or handler, logs the
// stack at error level with the request ID, and writes a minimal 500
// response. Must be outermost (before RequestID would also work but
// we want RequestID populated so the log line carries it).
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					reqID := RequestIDFrom(r.Context())
					logger.Error("panic",
						"request_id", reqID,
						"method", r.Method,
						"path", r.URL.Path,
						"panic", fmt.Sprintf("%v", rec),
						"stack", string(debug.Stack()),
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, `{"error":{"code":"server","message":"internal error","request_id":%q}}`, reqID)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
