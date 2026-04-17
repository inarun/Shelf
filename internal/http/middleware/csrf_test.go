package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testKey() []byte {
	return []byte("abcdefghijklmnopqrstuvwxyzABCDEF") // 32 bytes
}

func TestCSRFAllowsSafeMethods(t *testing.T) {
	h := Chain{RequestID, Session, CSRF(testKey())}.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("safe GET got %d", rec.Code)
	}
}

func TestCSRFRejectsUnsafeWithoutSession(t *testing.T) {
	h := Chain{RequestID, Session, CSRF(testKey())}.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler must not run")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/x", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST no session: status=%d want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"csrf"`) {
		t.Errorf("missing CSRF error envelope; body=%s", rec.Body.String())
	}
}

func TestCSRFAcceptsValidToken(t *testing.T) {
	// 1. Drive a GET to mint a cookie and compute the token.
	key := testKey()
	var cookie *http.Cookie
	var wantToken string
	driver := Chain{RequestID, Session}.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantToken = CSRFTokenFor(r.Context(), key)
	}))
	rec := httptest.NewRecorder()
	driver.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil || wantToken == "" {
		t.Fatalf("failed to prepare session/token (cookie=%v token=%q)", cookie, wantToken)
	}

	// 2. PATCH with that cookie + token must succeed.
	h := Chain{RequestID, Session, CSRF(key)}.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPatch, "/api/x", nil)
	req.AddCookie(cookie)
	req.Header.Set(CSRFHeader, wantToken)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNoContent {
		t.Errorf("valid PATCH got %d; body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestCSRFRejectsTamperedToken(t *testing.T) {
	key := testKey()
	var cookie *http.Cookie
	driver := Chain{RequestID, Session}.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	driver.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	h := Chain{RequestID, Session, CSRF(key)}.Then(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler must not run")
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req.AddCookie(cookie)
	req.Header.Set(CSRFHeader, "not-the-real-token-000000")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Errorf("tampered token: status=%d want 403", rec2.Code)
	}
}

func TestCSRFTokenForUnpopulatedContext(t *testing.T) {
	// If no session ever attached, token is "".
	if got := CSRFTokenFor(httptest.NewRequest(http.MethodGet, "/", nil).Context(), testKey()); got != "" {
		t.Errorf("unpopulated ctx should give empty token; got %q", got)
	}
}
