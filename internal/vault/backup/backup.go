package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// SnapshotInfo is the manifest of a completed backup.
type SnapshotInfo struct {
	// Root is the absolute path to the timestamped backup directory.
	Root string
	// Timestamp is the UTC moment used to name Root.
	Timestamp time.Time
	// Files lists paths of copied files relative to Root, in walk order.
	Files []string
	// Bytes is the total byte count copied.
	Bytes int64
}

// ErrDestinationUnderSource is returned by Snapshot when backupsRoot
// lives inside booksDir (misconfiguration that would cause the backup
// to recursively copy itself).
var ErrDestinationUnderSource = errors.New("backup: destination is inside the source")

// Snapshot copies every regular file under booksDir into a new
// timestamped directory under backupsRoot. The destination is
// books-{YYYYMMDDThhmmssZ}; if the UTC second already has a backup, the
// name gets a -NN suffix (up to -99) so simultaneous runs never silently
// merge into the same directory.
//
// booksDir and backupsRoot must be absolute paths to existing
// directories. Snapshot recurses into subdirectories and copies all
// regular-file entries, rebuilding the directory structure under the
// backup root. Symlinks, devices, and sockets are ignored.
//
// Each file copy goes through internal/vault/atomic.Write, so a partial
// snapshot (e.g., cancelled mid-run) leaves no half-written files
// behind — only a complete or missing copy under Root. The partial
// backup itself is preserved for operator inspection.
func Snapshot(ctx context.Context, booksDir, backupsRoot string) (*SnapshotInfo, error) {
	if !filepath.IsAbs(booksDir) {
		return nil, fmt.Errorf("backup: booksDir must be absolute: %q", booksDir)
	}
	if !filepath.IsAbs(backupsRoot) {
		return nil, fmt.Errorf("backup: backupsRoot must be absolute: %q", backupsRoot)
	}
	resolvedSource, err := filepath.EvalSymlinks(booksDir)
	if err != nil {
		return nil, fmt.Errorf("backup: resolving source: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(backupsRoot)
	if err != nil {
		return nil, fmt.Errorf("backup: resolving destination: %w", err)
	}

	// Refuse if backups root is inside source — would cause infinite
	// recursion and an unusable snapshot.
	if rel, err := filepath.Rel(resolvedSource, resolvedRoot); err == nil {
		clean := filepath.ToSlash(rel)
		if !strings.HasPrefix(clean, "../") && clean != ".." {
			return nil, ErrDestinationUnderSource
		}
	}

	stamp := time.Now().UTC()
	root, err := makeUniqueBackupRoot(resolvedRoot, stamp)
	if err != nil {
		return nil, err
	}

	info := &SnapshotInfo{Root: root, Timestamp: stamp}

	walkErr := filepath.WalkDir(resolvedSource, func(srcPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if strings.ContainsRune(srcPath, 0) {
			return fmt.Errorf("backup: null byte in path %q", srcPath)
		}

		rel, err := filepath.Rel(resolvedSource, srcPath)
		if err != nil {
			return fmt.Errorf("backup: rel %q: %w", srcPath, err)
		}
		dstPath := filepath.Join(root, rel)
		// Create parent directories first so the subsequent
		// ValidateWithinRoot's EvalSymlinks-on-parent has something to
		// resolve. Safe because `rel` is walker-yielded under the
		// already-resolved source root and cannot contain "..".
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o700); err != nil {
			return fmt.Errorf("backup: mkdir %q: %w", filepath.Dir(dstPath), err)
		}
		if _, err := paths.ValidateWithinRoot(root, dstPath); err != nil {
			return fmt.Errorf("backup: validate dst %q: %w", dstPath, err)
		}

		// #nosec G304 -- srcPath is yielded by filepath.WalkDir rooted at
		// resolvedSource (EvalSymlinks-canonicalized), so it cannot escape
		// the caller-supplied booksDir. Callers validate booksDir at config
		// load time; see package doc.
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("backup: read %q: %w", srcPath, err)
		}
		if err := atomic.Write(dstPath, data, 0o600); err != nil {
			return fmt.Errorf("backup: write %q: %w", dstPath, err)
		}

		info.Files = append(info.Files, rel)
		info.Bytes += int64(len(data))
		return nil
	})
	if walkErr != nil {
		return info, walkErr
	}
	return info, nil
}

// makeUniqueBackupRoot reserves a fresh directory named
// "books-{UTCstamp}" under parent. If the directory already exists
// (two snapshots within the same second), a -NN suffix is appended up
// to -99. Returns the absolute path of the reserved directory.
func makeUniqueBackupRoot(parent string, stamp time.Time) (string, error) {
	base := "books-" + stamp.Format("20060102T150405Z")
	for suffix := 0; suffix < 100; suffix++ {
		name := base
		if suffix > 0 {
			name = fmt.Sprintf("%s-%02d", base, suffix)
		}
		candidate := filepath.Join(parent, name)
		err := os.Mkdir(candidate, 0o700)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("backup: mkdir %q: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("backup: could not allocate unique directory under %q (100 attempts exhausted)", parent)
}
