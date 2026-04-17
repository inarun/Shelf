package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChainOrder(t *testing.T) {
	var order []string
	m1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m1-in")
			next.ServeHTTP(w, r)
			order = append(order, "m1-out")
		})
	}
	m2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m2-in")
			next.ServeHTTP(w, r)
			order = append(order, "m2-out")
		})
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "h")
	})
	Chain{m1, m2}.Then(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	want := []string{"m1-in", "m2-in", "h", "m2-out", "m1-out"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestRequestIDIsAttachedAndPropagated(t *testing.T) {
	var observed string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = RequestIDFrom(r.Context())
	})
	rec := httptest.NewRecorder()
	RequestID(h).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if observed == "" {
		t.Fatal("expected context-injected request ID")
	}
	if rec.Header().Get("X-Request-ID") != observed {
		t.Errorf("header id = %q, ctx id = %q", rec.Header().Get("X-Request-ID"), observed)
	}
	if len(observed) != 32 {
		t.Errorf("request id length = %d, want 32 hex chars", len(observed))
	}
}

func TestRecoverCatchesPanicReturns500(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelError}))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	rec := httptest.NewRecorder()
	Chain{RequestID, Recover(logger)}.Then(h).
		ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"server"`) {
		t.Errorf("missing server error envelope; body=%s", rec.Body.String())
	}
	if !strings.Contains(logBuf.String(), "panic") {
		t.Errorf("expected 'panic' in log; got %s", logBuf.String())
	}
}

func TestLoggingRecordsStatus(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	rec := httptest.NewRecorder()
	Chain{RequestID, Logging(logger)}.Then(h).
		ServeHTTP(rec, httptest.NewRequest("GET", "/foo", nil))
	out := logBuf.String()
	if !strings.Contains(out, "status=418") {
		t.Errorf("expected status=418 in log; got %s", out)
	}
	if !strings.Contains(out, "path=/foo") {
		t.Errorf("expected path in log; got %s", out)
	}
}
