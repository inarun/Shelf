package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
)

// CSRFHeader is the header clients must send on unsafe requests. It
// mirrors the token embedded in the HTML meta tag on every page.
const CSRFHeader = "X-CSRF-Token"

// CSRF enforces a per-session CSRF token on unsafe HTTP methods. The
// token is a deterministic derivation of the opaque session cookie
// value: hex(HMAC-SHA256(hmacKey, session_token)). Tokens are bound
// to the process lifetime because the hmacKey is regenerated on every
// restart (see server.NewKeys); returning tabs get 403 and must reload.
//
// Chain position: after Session so the session token is present in
// context, and after RequestID so the error envelope can cite it.
func CSRF(hmacKey []byte) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			sess := SessionTokenFrom(r.Context())
			if sess == "" {
				writeCSRFError(w, r, "missing session")
				return
			}
			want := csrfToken(hmacKey, sess)
			got := r.Header.Get(CSRFHeader)
			if got == "" || !hmac.Equal([]byte(got), []byte(want)) {
				writeCSRFError(w, r, "invalid or missing CSRF token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFTokenFor returns the token the client must echo for the given
// session. Handlers call this when rendering HTML to populate the
// <meta name="csrf-token"> tag. Returns "" when no session is present
// (which should never happen for a GET after Session middleware ran).
func CSRFTokenFor(ctx context.Context, hmacKey []byte) string {
	sess := SessionTokenFrom(ctx)
	if sess == "" {
		return ""
	}
	return csrfToken(hmacKey, sess)
}

func csrfToken(key []byte, session string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(session))
	return hex.EncodeToString(mac.Sum(nil))
}

func writeCSRFError(w http.ResponseWriter, r *http.Request, msg string) {
	reqID := RequestIDFrom(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `{"error":{"code":"csrf","message":%q,"request_id":%q}}`, msg, reqID)
}
