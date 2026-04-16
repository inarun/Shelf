package paths

import (
	"errors"
	"strings"
)

// ErrNonCanonical indicates a filename does not match the canonical
// "{Title} by {Author}.md" pattern. The indexer treats this as a warning
// (the file is still indexed but flagged in the UI) rather than a hard
// error, per SKILL.md §Filename. Future: a rename pipeline lets the user
// migrate non-canonical filenames with per-file confirmation.
var ErrNonCanonical = errors.New("filename is not canonical {Title} by {Author}.md")

const separator = " by "
const extension = ".md"

// Parse splits a canonical filename into (title, author). It strips the
// ".md" extension and splits on the LAST occurrence of " by " — titles
// commonly contain " by " ("Learning by Doing"), whereas author names
// rarely do, so last-occurrence maps a title-with-" by " to the right
// side of the split.
//
// Returns ErrNonCanonical if either the extension or separator is absent,
// or if title/author comes out empty after trimming.
func Parse(filename string) (title, author string, err error) {
	if !strings.HasSuffix(filename, extension) {
		return "", "", ErrNonCanonical
	}
	stem := strings.TrimSuffix(filename, extension)

	idx := strings.LastIndex(stem, separator)
	if idx < 0 {
		return "", "", ErrNonCanonical
	}

	title = strings.TrimSpace(stem[:idx])
	author = strings.TrimSpace(stem[idx+len(separator):])
	if title == "" || author == "" {
		return "", "", ErrNonCanonical
	}
	return title, author, nil
}
