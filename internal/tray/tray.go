// Package tray runs a Windows system-tray icon with a small popup
// menu: Open Shelf / Start with Windows (checkable) / Quit.
//
// Shelf integrates the tray so the user has a place to re-open the
// browser window after closing it, toggle autostart from a familiar
// affordance, and cleanly quit the background process. It is the only
// surface through which shelf.exe is long-lived on a typical session.
//
// As of v0.3.2 (Session 21), tray Quit is one of three peer shutdown
// triggers alongside SIGINT/SIGTERM and POST /api/shutdown. All three
// converge on the same cancel() + httpSrv.Shutdown(10s) + tray.Stop()
// sequence in cmd/shelf/main.go; no path bypasses the others.
//
// The Windows implementation is a direct Win32 integration against
// user32.dll / shell32.dll via golang.org/x/sys/windows. No cgo, no
// third-party tray libraries. See tray_windows.go for the syscalls and
// SKILL.md §Open Questions "System tray library selection" for the
// decision rationale. Non-Windows builds compile to a stub that
// returns ErrNotSupported from every function.
package tray

import "errors"

// ErrNotSupported is returned on non-Windows platforms. The unix stub
// returns this from Run so callers on developer laptops don't have to
// gate every tray call behind their own runtime.GOOS check.
var ErrNotSupported = errors.New("tray: not supported on this platform")

// Config bundles the callbacks the tray invokes in response to user
// interaction. Every callback is optional; nil callbacks are ignored.
// The tray does not own any app state — it forwards intent to the
// caller via these hooks.
type Config struct {
	// Tooltip is the hover text on the tray icon. Windows caps this at
	// 127 UTF-16 code units; anything longer is truncated by the API.
	Tooltip string

	// OnOpen fires when the user left-clicks the icon or selects
	// "Open Shelf" from the menu. Typical implementation: open the
	// browser at http://127.0.0.1:<port>/library.
	OnOpen func()

	// IsAutostartEnabled is consulted when the menu is built so the
	// "Start with Windows" item can show its checkmark. A nil hook is
	// treated as "not enabled". Must be cheap; the menu builds
	// synchronously on the tray's message-loop thread.
	IsAutostartEnabled func() bool

	// OnToggleAutostart fires when the user clicks the "Start with
	// Windows" item. The `enabled` argument is the requested new state
	// (the inverse of IsAutostartEnabled at click time). The hook is
	// called on a fresh goroutine so a slow registry write cannot
	// block the message loop; the returned error, if any, is the
	// caller's responsibility to surface.
	OnToggleAutostart func(enabled bool) error

	// OnQuit fires once when the user clicks "Quit" or when the tray
	// is otherwise shutting down on user action. It runs on a fresh
	// goroutine. Typical implementation: cancel the root context so
	// the HTTP server shuts down gracefully.
	OnQuit func()
}

// Run adds the tray icon and runs the message loop. It blocks until
// Stop is called or the user selects Quit. On non-Windows it returns
// ErrNotSupported immediately.
//
// Run internally locks the invoking goroutine to its OS thread
// (required for Win32 GUI) and never unlocks it; treat the goroutine
// that calls Run as single-purpose.
func Run(cfg Config) error { return run(cfg) }

// Stop requests an orderly shutdown of a running tray from any
// goroutine. It is safe to call when no tray is running (no-op). Stop
// itself does not wait; the Run goroutine returns once the Win32
// message loop has drained.
func Stop() { stop() }
