//go:build !windows

package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openURL resolves a platform-appropriate launcher binary and invokes
// it with the URL as a single literal argument. Per SKILL.md §Security
// controls, os/exec is only used with fully literal argument slices —
// the URL is validated by Open's caller (loopback-only) and is the
// single argument passed. No shell interpretation.
func openURL(u string) error {
	var bin string
	switch runtime.GOOS {
	case "darwin":
		bin = "open"
	default:
		bin = "xdg-open"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("browser: %s not on PATH: %w", bin, err)
	}
	// #nosec G204 -- path is LookPath-resolved; u is loopback-validated by Open.
	cmd := exec.Command(path, u)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("browser: start %s: %w", bin, err)
	}
	// Detach — we don't wait, we don't read stdout/stderr.
	_ = cmd.Process.Release()
	return nil
}
