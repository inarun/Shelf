package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// BinaryDir returns the directory containing the running executable,
// resolved through symlinks. Used to locate the portable-mode default for
// shelf.toml and data.directory.
//
// During `go test`, this returns the directory of the test binary — tests
// that rely on a specific layout should pass paths explicitly instead of
// using the default.
func BinaryDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("os.Executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// EvalSymlinks can fail on Windows for unusual filesystems;
		// the pre-resolution path is still a valid directory to Dir.
		resolved = exe
	}
	return filepath.Dir(resolved), nil
}
