//go:build !windows

package atomic

import "os"

// fsyncDir opens the directory and calls Sync. On POSIX this flushes the
// directory entry metadata to stable storage, making the prior rename
// durable across a crash.
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}
