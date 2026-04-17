package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// Logging records one access-log entry per request. Fields: method,
// path, status, duration_ms, request_id, remote_addr. Never logs bodies
// (SKILL.md §Security controls forbid review text in logs).
func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFrom(r.Context()),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

// statusResponseWriter captures the status code for the access log.
// Wrapped writers don't implement Flusher/Hijacker by default; the
// Shelf server doesn't need them (no SSE, no websockets) in Session 4.
type statusResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}
