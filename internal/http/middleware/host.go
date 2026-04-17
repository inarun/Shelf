package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Host returns a middleware that rejects requests whose Host header is
// not in the allowlist. This is the DNS-rebinding defense required by
// SKILL.md §Security controls: a localhost-only service must not serve
// requests addressed to arbitrary hostnames that happen to resolve to
// 127.0.0.1.
//
// The allowlist is always: "127.0.0.1:{port}" and "localhost:{port}"
// when bind == "127.0.0.1". When bind is something else (e.g.,
// "192.168.1.50"), the configured "{bind}:{port}" is added and the
// localhost entries remain (an operator reaching through a VPN may
// still hit the loopback).
//
// Mismatch → 421 Misdirected Request. Logs are emitted one level up
// via Logging, so Host itself does no logging.
func Host(bind string, port int) Middleware {
	allowed := buildHostAllowlist(bind, port)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hostAllowed(allowed, r.Host) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusMisdirectedRequest)
				reqID := RequestIDFrom(r.Context())
				fmt.Fprintf(w, `{"error":{"code":"host","message":"Host header not allowed","request_id":%q}}`, reqID)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func buildHostAllowlist(bind string, port int) map[string]struct{} {
	p := strconv.Itoa(port)
	out := map[string]struct{}{
		net.JoinHostPort("127.0.0.1", p): {},
		net.JoinHostPort("localhost", p): {},
	}
	if bind != "" && bind != "127.0.0.1" && bind != "localhost" {
		out[net.JoinHostPort(bind, p)] = struct{}{}
	}
	return out
}

func hostAllowed(allowed map[string]struct{}, host string) bool {
	// Normalize: strip any brackets around IPv6 literals and lowercase.
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false
	}
	_, ok := allowed[h]
	return ok
}
