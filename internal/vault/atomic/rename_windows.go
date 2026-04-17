//go:build windows

package atomic

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// renameWithRetry wraps os.Rename with a short retry loop keyed on
// ERROR_ACCESS_DENIED and ERROR_SHARING_VIOLATION. Both are transient
// Windows errors that commonly surface when:
//
//   - a concurrent writer just completed a rename to the same target
//     and the kernel hasn't fully released the previous file handle;
//   - antivirus or Windows Defender briefly scans the just-created
//     temp or target file.
//
// Backoff sequence: 1ms, 2ms, 4ms, 8ms, 16ms, 32ms, 64ms, 128ms, 256ms
// (roughly half a second cumulative). Longer than that almost certainly
// indicates a real contention problem and we should surface the error.
func renameWithRetry(src, dst string) error {
	const maxAttempts = 9
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := os.Rename(src, dst); err == nil {
			return nil
		} else {
			lastErr = err
			if !isRetryableWinError(err) {
				return err
			}
		}
		time.Sleep(time.Duration(1<<attempt) * time.Millisecond)
	}
	return lastErr
}

// ERROR_SHARING_VIOLATION is not exported by the standard syscall package
// (it lives in golang.org/x/sys/windows). Numeric value is stable Win32
// API, documented since the 1990s.
const errorSharingViolation syscall.Errno = 32

func isRetryableWinError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case syscall.ERROR_ACCESS_DENIED, errorSharingViolation:
		return true
	}
	return false
}

// Rename performs an atomic same-volume rename. On Windows it retries on
// transient ERROR_ACCESS_DENIED and ERROR_SHARING_VIOLATION (see
// renameWithRetry). Callers must ensure src and dst are on the same
// volume and that both paths have been validated via
// internal/vault/paths.ValidateWithinRoot (or the Vault wrapper) upstream.
func Rename(src, dst string) error {
	return renameWithRetry(src, dst)
}
