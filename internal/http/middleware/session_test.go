package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionIssuesCookieOnFirstGET(t *testing.T) {
	h := Session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionTokenFrom(r.Context()) == "" {
			t.Errorf("expected session token in context")
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != SessionCookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, SessionCookieName)
	}
	if !c.HttpOnly {
		t.Error("HttpOnly must be set")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
}

func TestSessionReusesExistingCookie(t *testing.T) {
	h := Session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "preexisting"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("expected no new cookie when one is already present")
	}
}

func TestSessionDoesNotIssueCookieOnUnsafeMethod(t *testing.T) {
	h := Session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("POST without cookie must not get one issued")
	}
}
