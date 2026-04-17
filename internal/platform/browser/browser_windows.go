//go:build windows

package browser

import (
	"fmt"
	"syscall"
	"unsafe"
)

// SW_SHOWNORMAL is the standard "activate and display the window in its
// current size and position" flag for ShellExecuteW.
const swShowNormal = 1

var (
	modShell32       = syscall.NewLazyDLL("shell32.dll")
	procShellExecute = modShell32.NewProc("ShellExecuteW")
)

// openURL hands the URL to ShellExecuteW with the "open" verb. The
// OS resolves the protocol handler for http/https and launches the
// default browser. No shell is involved; no subprocess is spawned by
// Shelf itself. The ShellExecute return value encodes success as a
// pointer-sized integer greater than 32.
func openURL(u string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("browser: utf16 verb: %w", err)
	}
	path, err := syscall.UTF16PtrFromString(u)
	if err != nil {
		return fmt.Errorf("browser: utf16 url: %w", err)
	}
	ret, _, callErr := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)), // #nosec G103 -- syscall pointer; UTF-16 buffer lives through Call.
		uintptr(unsafe.Pointer(path)), // #nosec G103 -- syscall pointer; same UTF-16 lifetime rule.
		0, 0,
		swShowNormal,
	)
	if ret > 32 {
		return nil
	}
	// Values 0..32 are documented error codes. callErr reflects the
	// syscall Errno via GetLastError; surface both so the operator can
	// diagnose "browser didn't open" reports.
	return fmt.Errorf("browser: ShellExecuteW failed (code=%d): %v", ret, callErr)
}
