package tray

import (
	"errors"
	"runtime"
	"testing"
)

// TestRunOnNonWindowsReturnsUnsupported is a compile-and-sanity check
// on builds where we don't have a full Win32 environment. On Windows
// this test is a no-op — we don't want to actually pop a tray icon
// during `go test`.
func TestRunOnNonWindowsReturnsUnsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping non-windows behaviour on windows build")
	}
	err := Run(Config{})
	if err == nil || !errors.Is(err, ErrNotSupported) {
		t.Errorf("Run() on %s = %v, want ErrNotSupported", runtime.GOOS, err)
	}
}

// TestStopNoActiveTray is safe everywhere: Stop is a no-op when no
// tray is running.
func TestStopNoActiveTray(t *testing.T) {
	Stop() // must not panic
}
