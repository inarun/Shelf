package note

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// Create writes a brand-new note to path. It serializes fm + b, then
// calls internal/vault/atomic.Write.
//
// Create refuses to overwrite an existing file: if os.Stat(path) succeeds
// (or reports anything other than "not exist") Create returns fs.ErrExist
// wrapped in a context error. The caller (e.g., the Goodreads importer's
// Apply path) treats that as a plan-vs-disk drift and surfaces it in the
// per-entry error list.
//
// path must already be validated via internal/vault/paths.ValidateWithinRoot
// (or the ValidateWithinVault wrapper). Create does not re-validate.
func Create(path string, fm *frontmatter.Frontmatter, b *body.Body) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("note: create %s: %w", path, fs.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("note: stat %s: %w", path, err)
	}

	data, err := fm.Serialize(b.Serialize())
	if err != nil {
		return fmt.Errorf("note: serialize %s: %w", path, err)
	}
	if err := atomic.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("note: write %s: %w", path, err)
	}
	return nil
}
