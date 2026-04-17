//go:build !windows

package atomic

import "os"

// renameWithRetry is just os.Rename on POSIX — rename is a single atomic
// syscall with no concurrent-contention quirks.
func renameWithRetry(src, dst string) error {
	return os.Rename(src, dst)
}

// Rename performs an atomic same-volume rename. On POSIX it's a thin
// wrapper over os.Rename; on Windows (see rename_windows.go) it retries
// on transient ERROR_ACCESS_DENIED and ERROR_SHARING_VIOLATION.
//
// Callers must ensure src and dst are on the same volume and that both
// paths have been validated via internal/vault/paths.ValidateWithinRoot
// (or the Vault wrapper) upstream.
func Rename(src, dst string) error {
	return renameWithRetry(src, dst)
}
