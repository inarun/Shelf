//go:build !windows

package atomic

import "os"

// renameWithRetry is just os.Rename on POSIX — rename is a single atomic
// syscall with no concurrent-contention quirks.
func renameWithRetry(src, dst string) error {
	return os.Rename(src, dst)
}
