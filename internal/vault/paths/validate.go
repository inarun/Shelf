package paths

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ValidateRelativeBooksFolder enforces that the books_folder config value
// is a safe vault-relative path: not absolute, no parent traversal, no
// drive letter, no UNC prefix, no null bytes. Callers should use this
// during config validation; the runtime equivalent for already-absolute
// paths is ValidateWithinVault.
func ValidateRelativeBooksFolder(rel string) error {
	if rel == "" {
		return errors.New("required")
	}
	if strings.ContainsRune(rel, 0) {
		return errors.New("contains a null byte")
	}
	// UNC prefix first — specific error beats the generic "root-like".
	if strings.HasPrefix(rel, `\\`) || strings.HasPrefix(rel, "//") {
		return fmt.Errorf("must not be a UNC path, got %q", rel)
	}
	// Leading path separator ("/foo" or "\foo") is root-relative — reject.
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return fmt.Errorf("must be vault-relative, got root-like path %q", rel)
	}
	// Windows drive-letter sneak ("D:other").
	if len(rel) >= 2 && rel[1] == ':' {
		return fmt.Errorf("must not include a drive letter, got %q", rel)
	}
	if filepath.IsAbs(rel) {
		return fmt.Errorf("must be vault-relative, got absolute path %q", rel)
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("must not escape the vault with .., got %q", rel)
	}
	return nil
}

// reservedNames is the set of Windows-reserved filename stems. Reserved
// regardless of extension: CON.txt is also invalid, as is COM1.md.
// Checked case-insensitively.
var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM0": true, "COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT0": true, "LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// isReservedWindowsName reports whether the filename component (stripped
// of any extension) matches a Windows-reserved device name.
func isReservedWindowsName(name string) bool {
	stem := name
	if dot := strings.Index(stem, "."); dot > 0 {
		stem = stem[:dot]
	}
	return reservedNames[strings.ToUpper(stem)]
}

// maxPathRunes mirrors the Windows MAX_PATH constant (260). Paths longer
// than this are rejected; long-path support (the `\\?\` prefix) is
// documented as future work.
const maxPathRunes = 260

// ValidateWithinVault resolves candidate to a canonical absolute path and
// asserts it lives under booksAbs. It rejects:
//
//   - null bytes anywhere in the input
//   - path traversal (..) that escapes the Books folder, both via string
//     cleaning and via symlink resolution on the parent directory
//   - reserved Windows names in any path component (CON, PRN, AUX, NUL,
//     COM0–9, LPT0–9; case-insensitive; reserved regardless of extension)
//   - paths longer than 260 runes (Windows MAX_PATH)
//
// Candidate may be relative (interpreted against booksAbs) or absolute.
// booksAbs must itself be an absolute, cleaned, existing directory — the
// caller is expected to have validated it at config-load time.
//
// For paths whose target file does not yet exist, symlink resolution runs
// on the parent directory. This handles the common case of "about to
// create a new book file" without requiring the file to exist first.
func ValidateWithinVault(booksAbs, candidate string) (string, error) {
	if strings.ContainsRune(candidate, 0) {
		return "", errors.New("path contains a null byte")
	}
	if !filepath.IsAbs(booksAbs) {
		return "", fmt.Errorf("books folder %q must be absolute", booksAbs)
	}

	abs := candidate
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(booksAbs, abs)
	}
	abs = filepath.Clean(abs)

	if utf8.RuneCountInString(abs) > maxPathRunes {
		return "", fmt.Errorf("path length %d exceeds MAX_PATH %d", utf8.RuneCountInString(abs), maxPathRunes)
	}

	// String-level prefix check catches pure ".." traversal even before
	// symlinks are involved; symlink resolution below catches escapes
	// that only appear after EvalSymlinks.
	relPre, err := filepath.Rel(filepath.Clean(booksAbs), abs)
	if err != nil {
		return "", fmt.Errorf("filepath.Rel: %w", err)
	}
	if relPre == ".." || strings.HasPrefix(relPre, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes books folder %q", abs, booksAbs)
	}

	resolvedBooks, err := filepath.EvalSymlinks(booksAbs)
	if err != nil {
		return "", fmt.Errorf("resolving books folder: %w", err)
	}

	// The leaf file may not exist yet; resolve symlinks on the parent
	// directory and append the leaf name.
	parent := filepath.Dir(abs)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("resolving parent %q: %w", parent, err)
	}
	resolvedAbs := filepath.Join(resolvedParent, filepath.Base(abs))

	// Canonical prefix check post-symlink-resolution.
	relPost, err := filepath.Rel(resolvedBooks, resolvedAbs)
	if err != nil {
		return "", fmt.Errorf("filepath.Rel (post-resolve): %w", err)
	}
	if relPost == ".." || strings.HasPrefix(relPost, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("resolved path %q escapes books folder %q (symlink escape)", resolvedAbs, resolvedBooks)
	}

	// Check every component of the resolved path (below booksAbs) for
	// reserved Windows names. Upstream components are the caller's
	// responsibility — they're part of the configured books path.
	for _, comp := range strings.Split(relPost, string(filepath.Separator)) {
		if comp == "" || comp == "." {
			continue
		}
		if isReservedWindowsName(comp) {
			return "", fmt.Errorf("path component %q is a reserved Windows name", comp)
		}
	}

	return resolvedAbs, nil
}
