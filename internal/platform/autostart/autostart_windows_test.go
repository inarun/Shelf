//go:build windows

package autostart

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// withScratchKey redirects the package-level keyPath to a fresh
// subkey under HKCU for the duration of the test and cleans it up
// after. Each test gets its own subkey so they can run in parallel.
func withScratchKey(t *testing.T) {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	sub := `Software\inarun-Shelf-test-` + hex.EncodeToString(b[:])
	prev := keyPath
	keyPath = sub
	// Create the subkey up front so the first operation sees it.
	k, _, err := registry.CreateKey(registry.CURRENT_USER, sub, registry.ALL_ACCESS)
	if err != nil {
		t.Fatalf("create scratch key: %v", err)
	}
	_ = k.Close()
	t.Cleanup(func() {
		keyPath = prev
		_ = registry.DeleteKey(registry.CURRENT_USER, sub)
	})
}

func TestEnableDisableStatus(t *testing.T) {
	withScratchKey(t)

	a, err := New("Shelf-test", `"C:\Shelf\shelf.exe"`)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Initially absent.
	enabled, cmd, err := a.Status()
	if err != nil {
		t.Fatalf("Status initial: %v", err)
	}
	if enabled {
		t.Fatal("initial Status: enabled=true, want false")
	}
	if cmd != "" {
		t.Errorf("initial Status: cmd = %q, want empty", cmd)
	}

	// Enable — status now present with expected command.
	if err := a.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	enabled, cmd, err = a.Status()
	if err != nil {
		t.Fatalf("Status after Enable: %v", err)
	}
	if !enabled {
		t.Error("Status after Enable: enabled=false")
	}
	if cmd != `"C:\Shelf\shelf.exe"` {
		t.Errorf("Status cmd = %q", cmd)
	}

	// Enable again — idempotent overwrite, not an error.
	if err := a.Enable(); err != nil {
		t.Fatalf("Enable (second): %v", err)
	}

	// Disable — status now absent again.
	if err := a.Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	enabled, _, err = a.Status()
	if err != nil {
		t.Fatalf("Status after Disable: %v", err)
	}
	if enabled {
		t.Error("Status after Disable: enabled=true")
	}

	// Disable again — idempotent no-op.
	if err := a.Disable(); err != nil {
		t.Errorf("Disable (second, nonexistent) = %v, want nil", err)
	}
}

func TestEnableOverwritesChangedCommand(t *testing.T) {
	withScratchKey(t)

	first, _ := New("Shelf-test", "cmd-one")
	if err := first.Enable(); err != nil {
		t.Fatalf("first Enable: %v", err)
	}
	second, _ := New("Shelf-test", "cmd-two")
	if err := second.Enable(); err != nil {
		t.Fatalf("second Enable: %v", err)
	}
	_, cmd, err := second.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if cmd != "cmd-two" {
		t.Errorf("after overwrite: cmd = %q, want cmd-two", cmd)
	}
}
