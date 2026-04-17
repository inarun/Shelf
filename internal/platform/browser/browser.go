// Package browser opens a URL in the user's default browser.
//
// Purpose-built for Shelf's single-instance behaviour: when a second
// process launches while a primary is already serving on the configured
// port, it opens the library URL in the default browser and exits.
//
// # Security
//
// Even though every caller in this repo constructs the URL from
// validated config (http://<bind>:<port>/<path>), Open rejects anything
// outside that shape as defense in depth. A future caller that forwards
// arbitrary input to this function cannot accidentally spawn a handler
// for file://, javascript:, or other dangerous schemes.
//
//   - Scheme must be http or https.
//   - Host must be a loopback literal: 127.0.0.1, ::1, or "localhost".
//     External binds are a warning-not-error per SKILL.md §Core
//     Invariants #4, but we do not open them from this package because
//     the single-instance code path runs in the context of "a user just
//     double-clicked the binary" — always the primary human session.
//   - No control characters, no embedded credentials.
//
// On Windows the URL is handed to ShellExecuteW directly; no subprocess,
// no cmd.exe. On Unix it runs xdg-open or `open` with a fully literal
// argument slice (SKILL.md §Security controls: os/exec allowed only
// with literal args).
package browser

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrUnsafeURL is returned when Open is given a URL that fails the
// loopback-only validation check. The caller should treat this as a
// programming bug, not a user-facing error — if it fires, something
// upstream is wrong.
var ErrUnsafeURL = errors.New("browser: URL failed loopback validation")

// Open validates the URL and asks the OS to open it in the default
// browser. It blocks only long enough to hand the URL to the OS and
// returns. A successful call does not guarantee the page ever loads —
// the OS may reject the protocol handler, the default browser may be
// missing, etc.
func Open(rawURL string) error {
	if err := validateLoopbackURL(rawURL); err != nil {
		return err
	}
	return openURL(rawURL)
}

// validateLoopbackURL enforces the Open contract. Kept separate so
// tests can exercise the validator without platform-specific side
// effects.
func validateLoopbackURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("%w: empty URL", ErrUnsafeURL)
	}
	for _, r := range rawURL {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: control character", ErrUnsafeURL)
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: parse: %v", ErrUnsafeURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q not allowed", ErrUnsafeURL, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: userinfo present", ErrUnsafeURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: no host", ErrUnsafeURL)
	}
	if !isLoopback(host) {
		return fmt.Errorf("%w: host %q is not loopback", ErrUnsafeURL, host)
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
