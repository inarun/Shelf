package handlers

import (
	"net"
	"net/http"
)

// Shutdown handles POST /api/shutdown — the web-UI path for quitting
// Shelf, peer to SIGINT/SIGTERM and tray Quit.
//
// Hardening. In addition to the global CSRF middleware (which already
// rejects un-tokened POSTs with 403 "csrf"), this handler asserts that
// r.RemoteAddr is a loopback IP (net.IP.IsLoopback — covers 127.0.0.0/8
// and ::1). The Host middleware allowlist is the primary DNS-rebind
// defense; a future widening of that allowlist (v0.6 Tailscale) must
// not accidentally widen shutdown exposure. Belt-and-suspenders.
//
// Response. 202 Accepted with {"status":"shutting_down"} is written
// and flushed before the channel is signalled so the client sees the
// acknowledgement even when main.go tears the server down immediately.
// The shutdown channel is buffered(1); a select-default drops
// duplicate clicks, so rapid double-submits still return 202.
func (d *Dependencies) Shutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		d.writeJSONError(w, r, http.StatusMethodNotAllowed, "invalid", "POST only")
		return
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		d.writeJSONError(w, r, http.StatusForbidden, "forbidden", "shutdown requires loopback origin")
		return
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		d.writeJSONError(w, r, http.StatusForbidden, "forbidden", "shutdown requires loopback origin")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte(`{"status":"shutting_down"}`)); err != nil {
		d.Logger.Debug("shutdown response write failed", "err", err)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	if d.ShutdownSignal == nil {
		d.Logger.Warn("shutdown handler invoked but ShutdownSignal is nil")
		return
	}
	select {
	case d.ShutdownSignal <- struct{}{}:
		d.Logger.Info("web-ui shutdown triggered", "remote", r.RemoteAddr)
	default:
		d.Logger.Debug("web-ui shutdown re-request ignored (already signaled)")
	}
}
