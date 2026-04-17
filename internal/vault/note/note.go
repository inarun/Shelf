package note

import (
	"errors"

	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// ErrStale is returned by SaveBody when the file has changed on disk
// since Read. Callers (the UI) should prompt the user to reload before
// saving. SaveFrontmatter never returns ErrStale — frontmatter-only
// writes are unconditional per SKILL.md §Concurrent edit handling.
var ErrStale = errors.New("note: file changed on disk since last read")

// Note is a parsed book file with the staleness pair stamped at read
// time. Path is always an absolute path that has already been validated
// by internal/vault/paths.ValidateWithinVault; this package does not
// re-validate.
type Note struct {
	Path        string
	Size        int64
	MtimeNanos  int64
	Frontmatter *frontmatter.Frontmatter
	Body        *body.Body
}
