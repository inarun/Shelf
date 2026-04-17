package singleton

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// freePort binds 127.0.0.1:0, reads the assigned port, and closes the
// listener. The caller then has a port that was free moments ago —
// race-prone in theory but fine for local unit tests on a quiet machine.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func TestProbeNoListener(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	err := Probe(context.Background(), port, "shelf ok")
	if err == nil || !errors.Is(err, ErrNoPrimary) {
		t.Errorf("Probe no listener = %v, want ErrNoPrimary", err)
	}
}

// startHealthServer spins up a short-lived HTTP server on 127.0.0.1:port
// serving /healthz with the given body and status.
func startHealthServer(t *testing.T, port int, status int, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen on %d: %v", port, err)
	}
	srv := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second},
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeSignatureMatch(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	_ = startHealthServer(t, port, http.StatusOK, "shelf ok")
	if err := Probe(context.Background(), port, "shelf ok"); err != nil {
		t.Errorf("Probe signature match = %v, want nil", err)
	}
}

func TestProbeSignatureMismatch(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	_ = startHealthServer(t, port, http.StatusOK, "pong")
	err := Probe(context.Background(), port, "shelf ok")
	if err == nil || !errors.Is(err, ErrNoPrimary) {
		t.Errorf("Probe wrong body = %v, want ErrNoPrimary", err)
	}
}

func TestProbeNon200(t *testing.T) {
	t.Parallel()
	port := freePort(t)
	_ = startHealthServer(t, port, http.StatusInternalServerError, "shelf ok")
	err := Probe(context.Background(), port, "shelf ok")
	if err == nil || !errors.Is(err, ErrNoPrimary) {
		t.Errorf("Probe non-200 = %v, want ErrNoPrimary", err)
	}
}

func TestProbeLargeBodyTruncated(t *testing.T) {
	t.Parallel()
	// A server that returns a ton of data; signature is within the
	// first readLimit bytes so match still works, but the ReadAll
	// must not balloon unbounded.
	port := freePort(t)
	body := "shelf ok" + strings.Repeat("x", 1<<20)
	_ = startHealthServer(t, port, http.StatusOK, body)
	if err := Probe(context.Background(), port, "shelf ok"); err != nil {
		t.Errorf("Probe with bloated body = %v, want nil", err)
	}
}

func TestProbeRejectsBadPort(t *testing.T) {
	t.Parallel()
	err := Probe(context.Background(), 0, "shelf ok")
	if err == nil || !errors.Is(err, ErrNoPrimary) {
		t.Errorf("Probe bad port = %v, want ErrNoPrimary", err)
	}
	err = Probe(context.Background(), 70000, "shelf ok")
	if err == nil || !errors.Is(err, ErrNoPrimary) {
		t.Errorf("Probe oversize port = %v, want ErrNoPrimary", err)
	}
}

func TestProbeRejectsEmptySignature(t *testing.T) {
	t.Parallel()
	err := Probe(context.Background(), 7744, "")
	if err == nil || !errors.Is(err, ErrNoPrimary) {
		t.Errorf("Probe empty sig = %v, want ErrNoPrimary", err)
	}
}
