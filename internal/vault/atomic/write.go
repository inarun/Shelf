package atomic

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Write writes data to path atomically: write to a temp file in the same
// directory, fsync it, then os.Rename to the target. Temp file uses a
// crypto/rand suffix so concurrent writers don't collide. On any error
// before the rename completes, the temp file is removed via a deferred
// cleanup so panics and early returns both leave no orphan .shelf.tmp
// files behind.
//
// The caller is expected to have pre-validated path via
// internal/vault/paths.ValidateWithinVault. This function does not
// re-validate — path validation is a cross-cutting concern handled at
// the API boundaries, not inside every writer.
//
// Write does NOT create parent directories. Callers should ensure the
// parent exists; failing loud here surfaces a misconfigured caller
// rather than silently creating unexpected directories.
//
// Windows caveat: directory fsync is not supported by the OS and is a
// no-op. os.Rename on Windows uses MoveFileExW which replaces the target
// atomically; durability of the rename itself is at the filesystem's
// discretion. This is acceptable for v0.1's data, which is reconstructible
// from the vault in the worst case.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	var randBytes [8]byte
	if _, err := rand.Read(randBytes[:]); err != nil {
		return fmt.Errorf("atomic: random: %w", err)
	}
	tmpName := fmt.Sprintf(".%s.%s.shelf.tmp", base, hex.EncodeToString(randBytes[:]))
	tmpPath := filepath.Join(dir, tmpName)

	// #nosec G304 -- tmpPath is constructed from the pre-validated `path`
	// argument (callers run internal/vault/paths.ValidateWithinVault first).
	// Path validation is intentionally done at the API boundary, not inside
	// every writer; see the package doc.
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("atomic: create temp %s: %w", tmpPath, err)
	}

	// `renamed` flips to true only after os.Rename succeeds. The defer
	// cleans up the temp file on any other outcome, including panics that
	// unwind through this function.
	renamed := false
	defer func() {
		_ = f.Close() // safe to call twice; the explicit Close below races with panic unwind
		if !renamed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("atomic: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("atomic: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("atomic: close: %w", err)
	}

	if err := renameWithRetry(tmpPath, path); err != nil {
		return fmt.Errorf("atomic: rename %s -> %s: %w", tmpPath, path, err)
	}
	renamed = true

	if err := fsyncDir(dir); err != nil {
		// The rename already happened; on POSIX the change may not be
		// durable until the parent directory is fsynced. Return the error
		// so the caller knows durability wasn't guaranteed. On Windows
		// fsyncDir is a no-op.
		return fmt.Errorf("atomic: fsync dir %s: %w", dir, err)
	}
	return nil
}
