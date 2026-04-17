package note

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// SaveBody writes the full note (frontmatter + body) back to disk
// atomically. Refuses the write with ErrStale if the file's current
// (size, mtime_ns) pair differs from what was stamped at Read time;
// the UI must prompt the user to reload in that case.
func (n *Note) SaveBody() error {
	stat, err := os.Stat(n.Path)
	if err != nil {
		return fmt.Errorf("note: stat %s: %w", n.Path, err)
	}
	if stat.Size() != n.Size || stat.ModTime().UnixNano() != n.MtimeNanos {
		return ErrStale
	}

	data, err := n.Frontmatter.Serialize(n.Body.Serialize())
	if err != nil {
		return fmt.Errorf("note: serialize %s: %w", n.Path, err)
	}

	if err := atomic.Write(n.Path, data, 0o600); err != nil {
		return fmt.Errorf("note: write %s: %w", n.Path, err)
	}

	return n.restamp()
}

// SaveFrontmatter replays the app's frontmatter mutations onto a fresh
// disk copy of the file and writes atomically. This path never returns
// ErrStale — by construction it reads the file again and preserves any
// concurrent edit to untouched frontmatter fields or to the body. The
// in-memory Body may be stale relative to disk after this call; callers
// who care should Read() again before displaying.
func (n *Note) SaveFrontmatter() error {
	// #nosec G304 -- n.Path was validated at Read time.
	f, err := os.Open(n.Path)
	if err != nil {
		return fmt.Errorf("note: reopen %s: %w", n.Path, err)
	}
	data, readErr := io.ReadAll(f)
	_ = f.Close()
	if readErr != nil {
		return fmt.Errorf("note: reread %s: %w", n.Path, readErr)
	}

	freshFm, freshBody, err := frontmatter.Parse(data)
	if err != nil && !errors.Is(err, frontmatter.ErrNoFrontmatter) {
		return fmt.Errorf("note: parse on reread %s: %w", n.Path, err)
	}
	if freshFm == nil {
		freshFm = frontmatter.NewEmpty()
	}

	for _, key := range n.Frontmatter.MutatedKeys() {
		v := n.Frontmatter.GetRawValue(key)
		if v == nil {
			continue
		}
		freshFm.SetRawValue(key, v)
	}

	out, err := freshFm.Serialize(freshBody)
	if err != nil {
		return fmt.Errorf("note: serialize %s: %w", n.Path, err)
	}

	if err := atomic.Write(n.Path, out, 0o600); err != nil {
		return fmt.Errorf("note: write %s: %w", n.Path, err)
	}

	return n.restamp()
}

// restamp re-reads size + mtime after a successful write so subsequent
// SaveBody calls don't false-positive as stale.
func (n *Note) restamp() error {
	stat, err := os.Stat(n.Path)
	if err != nil {
		return fmt.Errorf("note: restat %s: %w", n.Path, err)
	}
	n.Size = stat.Size()
	n.MtimeNanos = stat.ModTime().UnixNano()
	return nil
}
