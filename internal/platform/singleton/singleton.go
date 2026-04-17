// Package singleton determines whether another Shelf process is
// already serving on the configured port before we attempt to bind.
//
// The primary Shelf process on a user's machine is typically started
// by autostart-on-login or by the user double-clicking shelf.exe.
// Subsequent launches (e.g., a second double-click from the Start
// menu) should not produce a "port already in use" dialog — they
// should open the library page in the default browser and exit.
//
// The check is done with a short dial + HTTP GET /healthz probe. If
// the body contains the Shelf signature token, we treat the existing
// process as the authoritative primary. Anything else — dial error,
// timeout, wrong signature, non-200 — means either nothing is there
// or something unrelated is, and the caller should proceed to bind
// normally. A genuine collision with an unrelated service will then
// surface as a clear bind error rather than a silent misroute.
package singleton

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Defaults keep the probe fast: startup must not stall by more than
// about a second on a cold check. Windows loopback dials complete in
// microseconds under normal conditions.
const (
	dialTimeout    = 500 * time.Millisecond
	requestTimeout = 2 * time.Second
	readLimit      = 256
)

// ErrNoPrimary is returned when no Shelf process is responding on the
// target port. This is the signal for the caller to proceed as the
// primary itself.
var ErrNoPrimary = errors.New("singleton: no existing Shelf primary")

// Probe opens a TCP connection to 127.0.0.1:port (always loopback,
// never the configured external bind) and issues GET /healthz. It
// returns nil if the response includes signature, indicating an
// existing Shelf primary. Any other outcome — dial failure, timeout,
// non-200 status, or signature mismatch — returns an error wrapping
// ErrNoPrimary.
func Probe(ctx context.Context, port int, signature string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("%w: port %d out of range", ErrNoPrimary, port)
	}
	if signature == "" {
		return fmt.Errorf("%w: empty signature", ErrNoPrimary)
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("%w: dial %s: %v", ErrNoPrimary, addr, err)
	}
	_ = conn.Close()

	// New context for the HTTP GET so a slow response doesn't hold
	// the dial context past its timeout.
	getCtx, cancelGet := context.WithTimeout(ctx, requestTimeout)
	defer cancelGet()

	client := &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
			TLSHandshakeTimeout:   requestTimeout,
			ResponseHeaderTimeout: requestTimeout,
			DisableKeepAlives:     true,
		},
	}
	u := url.URL{Scheme: "http", Host: addr, Path: "/healthz"}
	req, err := http.NewRequestWithContext(getCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrNoPrimary, err)
	}
	req.Header.Set("User-Agent", "shelf-singleton-probe")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: get /healthz: %v", ErrNoPrimary, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrNoPrimary, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, readLimit))
	if err != nil {
		return fmt.Errorf("%w: read body: %v", ErrNoPrimary, err)
	}
	if !bytes.Contains(body, []byte(signature)) {
		return fmt.Errorf("%w: signature %q not in body (%q)", ErrNoPrimary, signature, body)
	}
	return nil
}
