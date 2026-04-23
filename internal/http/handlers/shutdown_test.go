package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newShutdownReq returns a POST /api/shutdown request with the given
// RemoteAddr so each test can steer the loopback check independently.
// httptest.NewRequest defaults RemoteAddr to 192.0.2.1:1234 (TEST-NET-1);
// every shutdown test either keeps that non-loopback value or overrides
// with 127.0.0.1 / [::1].
func newShutdownReq(method, remoteAddr string) *http.Request {
	r := httptest.NewRequest(method, "/api/shutdown", nil)
	r.RemoteAddr = remoteAddr
	return r
}

// attachShutdownChan swaps d.ShutdownSignal for a fresh buffered(1)
// channel and returns the bidirectional end so the test can observe
// signals without racing the handler.
func attachShutdownChan(d *Dependencies) chan struct{} {
	ch := make(chan struct{}, 1)
	d.ShutdownSignal = ch
	return ch
}

func TestShutdown_Returns405OnGet(t *testing.T) {
	d, _ := seedDeps(t)
	attachShutdownChan(d)

	rec := httptest.NewRecorder()
	d.Shutdown(rec, newShutdownReq(http.MethodGet, "127.0.0.1:54321"))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 body=%s", rec.Code, rec.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, rec.Body.String())
	}
	if env.Error.Code != "invalid" {
		t.Errorf("error code = %q, want invalid", env.Error.Code)
	}
}

func TestShutdown_NonLoopbackRemote_Rejected(t *testing.T) {
	d, _ := seedDeps(t)
	ch := attachShutdownChan(d)

	rec := httptest.NewRecorder()
	d.Shutdown(rec, newShutdownReq(http.MethodPost, "192.168.1.50:12345"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, rec.Body.String())
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("error code = %q, want forbidden", env.Error.Code)
	}
	// Channel must be untouched.
	select {
	case <-ch:
		t.Fatal("shutdown channel fired for non-loopback request")
	default:
	}
}

func TestShutdown_MalformedRemoteAddr_Rejected(t *testing.T) {
	d, _ := seedDeps(t)
	ch := attachShutdownChan(d)

	rec := httptest.NewRecorder()
	d.Shutdown(rec, newShutdownReq(http.MethodPost, "not-a-host-port"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-ch:
		t.Fatal("shutdown channel fired on malformed RemoteAddr")
	default:
	}
}

func TestShutdown_IPv4LoopbackAllowed(t *testing.T) {
	d, _ := seedDeps(t)
	ch := attachShutdownChan(d)

	rec := httptest.NewRecorder()
	d.Shutdown(rec, newShutdownReq(http.MethodPost, "127.0.0.1:50000"))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("shutdown channel did not fire within 100ms")
	}
}

func TestShutdown_IPv6LoopbackAllowed(t *testing.T) {
	d, _ := seedDeps(t)
	ch := attachShutdownChan(d)

	rec := httptest.NewRecorder()
	d.Shutdown(rec, newShutdownReq(http.MethodPost, "[::1]:50000"))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("shutdown channel did not fire for IPv6 loopback")
	}
}

// TestShutdown_NonLoopback127_2Accepted documents the intentional
// reliance on net.IP.IsLoopback() which treats all of 127.0.0.0/8 as
// loopback — not just the single 127.0.0.1 literal. If this test ever
// needs to flip to Rejected, it's a conscious spec change: tighten
// the check to a literal-string match AND update SKILL.md §v0.3.2.
func TestShutdown_NonLoopback127_2Accepted(t *testing.T) {
	d, _ := seedDeps(t)
	ch := attachShutdownChan(d)

	rec := httptest.NewRecorder()
	d.Shutdown(rec, newShutdownReq(http.MethodPost, "127.0.0.2:50000"))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (127/8 is loopback per RFC 6890) body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("shutdown channel did not fire for 127.0.0.2")
	}
}

func TestShutdown_HappyPath_SignalsShutdownChannel(t *testing.T) {
	d, _ := seedDeps(t)
	ch := attachShutdownChan(d)

	rec := httptest.NewRecorder()
	d.Shutdown(rec, newShutdownReq(http.MethodPost, "127.0.0.1:50000"))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v body=%s", err, rec.Body.String())
	}
	if body.Status != "shutting_down" {
		t.Errorf("status = %q, want shutting_down", body.Status)
	}
	select {
	case <-ch:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("shutdown channel did not fire")
	}
}

func TestShutdown_DuplicateRequestsAreIdempotent(t *testing.T) {
	d, _ := seedDeps(t)
	ch := attachShutdownChan(d)

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		d.Shutdown(rec, newShutdownReq(http.MethodPost, "127.0.0.1:50000"))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("attempt %d status = %d, want 202 body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	// Exactly one signal should have landed (buffer size 1, select-default
	// drops the second send).
	select {
	case <-ch:
	default:
		t.Fatal("expected one signal in the channel after two POSTs")
	}
	select {
	case <-ch:
		t.Fatal("unexpected second signal — duplicate send was not dropped")
	default:
	}
}

func TestShutdown_NilChannel_StillReturns202(t *testing.T) {
	// Defensive: if main.go ever wires a nil channel, the handler must
	// still respond and log a warning rather than panic. Same-origin
	// loopback request should still return 202; the handler's nil-guard
	// logs the situation and returns without signalling.
	d, _ := seedDeps(t)
	d.ShutdownSignal = nil

	rec := httptest.NewRecorder()
	d.Shutdown(rec, newShutdownReq(http.MethodPost, "127.0.0.1:50000"))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 body=%s", rec.Code, rec.Body.String())
	}
}
