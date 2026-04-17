package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersAreSet(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	checks := map[string]string{
		"Content-Security-Policy":      CSPPolicy,
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"X-Frame-Options":              "DENY",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	for header, want := range checks {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestCSPForbidsInlineScriptSourcesAndFraming(t *testing.T) {
	// Spot-checks that the CSP string includes the key SKILL.md-required
	// directives. If these fragments ever drift we want the test to scream.
	required := []string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self'",
		"img-src 'self' data:",
		"connect-src 'self'",
		"frame-ancestors 'none'",
	}
	for _, frag := range required {
		if !strings.Contains(CSPPolicy, frag) {
			t.Errorf("CSPPolicy missing %q", frag)
		}
	}
	for _, forbidden := range []string{"unsafe-inline", "unsafe-eval", "*"} {
		if strings.Contains(CSPPolicy, forbidden) {
			t.Errorf("CSPPolicy contains forbidden directive %q", forbidden)
		}
	}
}

func TestSecurityHeadersAppliedOnErrorStatus(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("CSP must be present on error responses too")
	}
}
