package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeySessionToken
)

// RequestID attaches a 16-byte random hex ID to the request context
// and the X-Request-ID response header. Every later middleware/handler
// should include it in log records so one user-reported failure maps
// to one log line.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom returns the request ID attached by RequestID, or "" if
// the middleware was not in the chain (shouldn't happen in production).
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand read failure is catastrophic; return an obvious sentinel
		// so the ID is visible in logs but the request still proceeds.
		return "rng-unavailable"
	}
	return hex.EncodeToString(b[:])
}
