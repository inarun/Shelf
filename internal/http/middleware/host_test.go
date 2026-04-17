package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostAllowlistLoopback(t *testing.T) {
	h := Host("127.0.0.1", 7744)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	cases := []struct {
		host     string
		wantCode int
	}{
		{"127.0.0.1:7744", http.StatusNoContent},
		{"localhost:7744", http.StatusNoContent},
		{"LOCALHOST:7744", http.StatusNoContent},
		{"127.0.0.1:9999", http.StatusMisdirectedRequest},
		{"evil.example:7744", http.StatusMisdirectedRequest},
		{"", http.StatusMisdirectedRequest},
		{"attacker", http.StatusMisdirectedRequest},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = tc.host
		rec := httptest.NewRecorder()
		Chain{RequestID}.Then(h).ServeHTTP(rec, req)
		if rec.Code != tc.wantCode {
			t.Errorf("host=%q got status=%d want=%d", tc.host, rec.Code, tc.wantCode)
		}
	}
}

func TestHostAllowlistCustomBind(t *testing.T) {
	h := Host("192.168.1.50", 9999)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	cases := []struct {
		host     string
		wantCode int
	}{
		{"192.168.1.50:9999", http.StatusNoContent},
		{"127.0.0.1:9999", http.StatusNoContent},
		{"localhost:9999", http.StatusNoContent},
		{"192.168.1.50:7744", http.StatusMisdirectedRequest},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = tc.host
		rec := httptest.NewRecorder()
		Chain{RequestID}.Then(h).ServeHTTP(rec, req)
		if rec.Code != tc.wantCode {
			t.Errorf("host=%q got status=%d want=%d", tc.host, rec.Code, tc.wantCode)
		}
	}
}
