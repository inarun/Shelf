package paths

import "errors"

// ErrEmptyComponent indicates that a filename component (title or author)
// collapsed to empty after sanitization. Callers should surface this as a
// validation error rather than falling back to a placeholder filename.
var ErrEmptyComponent = errors.New("filename component is empty after sanitization")

// Generate returns the canonical book filename per SKILL.md §Filename:
// "{SanitizeFilename(title)} by {SanitizeFilename(author)}.md".
//
// For books with multiple authors, callers pass only the first author
// (the full list lives in frontmatter). Callers that want a filename
// inside the Books folder should join the result with the validated
// books path via path/filepath.Join, then run it through
// ValidateWithinVault before any filesystem operation.
func Generate(title, author string) (string, error) {
	t := SanitizeFilename(title)
	if t == "" {
		return "", ErrEmptyComponent
	}
	a := SanitizeFilename(author)
	if a == "" {
		return "", ErrEmptyComponent
	}
	return t + " by " + a + ".md", nil
}
