package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
)

// SessionCookieName is the fixed name of the opaque session cookie.
// It has no authz meaning; it exists solely to bind CSRF tokens to a
// per-browser session.
const SessionCookieName = "shelf_session"

// Session issues and reads the shelf_session cookie. On any request
// missing the cookie, a safe method (GET/HEAD/OPTIONS) gets a fresh
// cookie set; unsafe methods are left cookie-less so CSRF rejects
// them cleanly (no token can match).
//
// The cookie is HttpOnly + SameSite=Strict + Path=/ with no Max-Age
// (session-scoped). Secure is intentionally off because the server
// is localhost-only HTTP; setting Secure would block the cookie.
func Session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := readSessionCookie(r)
		if token == "" && isSafeMethod(r.Method) {
			token = newSessionToken()
			// #nosec G124 -- Secure=false is intentional: Shelf binds to
			// localhost HTTP by design (SKILL.md §Core Invariants #4). The
			// Secure flag would prevent the browser from sending the cookie
			// over HTTP at all, breaking the one-user local app. HttpOnly +
			// SameSite=Strict still defend against XSS exfiltration and
			// cross-site requests; there is no network-attacker threat on a
			// loopback socket.
			http.SetCookie(w, &http.Cookie{
				Name:     SessionCookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   false,
			})
		}
		ctx := context.WithValue(r.Context(), ctxKeySessionToken, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SessionTokenFrom returns the opaque session token attached by Session
// middleware (possibly empty if the cookie was missing on an unsafe
// request).
func SessionTokenFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeySessionToken).(string); ok {
		return v
	}
	return ""
}

func readSessionCookie(r *http.Request) string {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c == nil {
		return ""
	}
	return c.Value
}

func newSessionToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
