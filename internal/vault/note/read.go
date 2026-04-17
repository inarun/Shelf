package note

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// Read loads a book note from disk and stamps its (size, mtime_ns) pair
// for the concurrent-edit guard. The stat is taken from the open file
// handle — same syscall produces both the content and the pair — so
// there's no read-stat race. Files with no frontmatter are still loaded
// with an empty Frontmatter so callers can edit and save them.
func Read(path string) (*Note, error) {
	// #nosec G304 -- path is pre-validated by internal/vault/paths.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("note: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("note: stat %s: %w", path, err)
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("note: read %s: %w", path, err)
	}

	fm, rawBody, err := frontmatter.Parse(data)
	if err != nil && !errors.Is(err, frontmatter.ErrNoFrontmatter) {
		return nil, fmt.Errorf("note: frontmatter %s: %w", path, err)
	}
	if fm == nil {
		fm = frontmatter.NewEmpty()
	}

	bd, err := body.Parse(rawBody)
	if err != nil {
		return nil, fmt.Errorf("note: body %s: %w", path, err)
	}

	return &Note{
		Path:        path,
		Size:        stat.Size(),
		MtimeNanos:  stat.ModTime().UnixNano(),
		Frontmatter: fm,
		Body:        bd,
	}, nil
}
