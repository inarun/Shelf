//go:build windows

package autostart

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// runKeyPath is the relative subkey under HKCU for per-user autostart
// applications. Constant so tests can swap it via the testing.go hook
// without modifying this file.
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// runKeyFor returns the HKCU + subkey used for real operations. Tests
// override keyPath to sandbox writes under a scratch subkey.
var keyPath = runKeyPath

func openRunKey(access uint32) (registry.Key, error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, access)
	if err != nil {
		return 0, fmt.Errorf("autostart: open HKCU\\%s: %w", keyPath, err)
	}
	return k, nil
}

func (a *Autostart) enable() error {
	k, err := openRunKey(registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	if err := k.SetStringValue(a.name, a.command); err != nil {
		return fmt.Errorf("autostart: set value %q: %w", a.name, err)
	}
	return nil
}

func (a *Autostart) disable() error {
	k, err := openRunKey(registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	if err := k.DeleteValue(a.name); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil // idempotent — nothing to remove
		}
		return fmt.Errorf("autostart: delete value %q: %w", a.name, err)
	}
	return nil
}

func (a *Autostart) status() (bool, string, error) {
	k, err := openRunKey(registry.QUERY_VALUE)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = k.Close() }()
	val, _, err := k.GetStringValue(a.name)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("autostart: read value %q: %w", a.name, err)
	}
	return true, val, nil
}
