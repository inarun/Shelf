package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setupBooks creates a temp directory acting as the absolute "books folder"
// and returns its canonical absolute path.
func setupBooks(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	books := filepath.Join(dir, "Books")
	if err := os.MkdirAll(books, 0o700); err != nil {
		t.Fatal(err)
	}
	// EvalSymlinks to handle OS-level canonicalization (e.g., /private/var on macOS).
	resolved, err := filepath.EvalSymlinks(books)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestValidateRelativeBooksFolder_Valid(t *testing.T) {
	cases := []string{
		"Books",
		"Subdir/Books",
		"2 - Source Material/Books",
		`2 - Source Material\Books`,
		"deeply/nested/books/folder",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if err := ValidateRelativeBooksFolder(c); err != nil {
				t.Errorf("unexpected error for %q: %v", c, err)
			}
		})
	}
}

func TestValidateRelativeBooksFolder_Invalid(t *testing.T) {
	cases := []struct {
		in         string
		wantSubstr string
	}{
		{"", "required"},
		{"\x00hidden", "null byte"},
		{`\\server\share`, "UNC"},
		{"//server/share", "UNC"},
		{"/root-relative", "root-like"},
		{`\root-relative`, "root-like"},
		{"D:other", "drive letter"},
		{"C:/Windows", "drive letter"},
		{"../escape", "escape the vault"},
		{"../../../etc", "escape the vault"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			err := ValidateRelativeBooksFolder(c.in)
			if err == nil {
				t.Fatalf("expected error for %q", c.in)
			}
			if !strings.Contains(err.Error(), c.wantSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), c.wantSubstr)
			}
		})
	}
}

func TestValidateWithinVault_Happy(t *testing.T) {
	books := setupBooks(t)
	cases := []string{
		"Hyperion by Dan Simmons.md",
		"Subfolder/Book.md",
	}
	for _, rel := range cases {
		t.Run(rel, func(t *testing.T) {
			// Create parent directory if the candidate is nested.
			parent := filepath.Dir(filepath.Join(books, rel))
			if err := os.MkdirAll(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			got, err := ValidateWithinVault(books, rel)
			if err != nil {
				t.Fatalf("ValidateWithinVault: %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("returned path not absolute: %q", got)
			}
			if !strings.HasPrefix(got, books) {
				t.Errorf("returned path %q not under books %q", got, books)
			}
		})
	}
}

func TestValidateWithinVault_AbsoluteCandidate(t *testing.T) {
	books := setupBooks(t)
	abs := filepath.Join(books, "Nested by Dan.md")
	got, err := ValidateWithinVault(books, abs)
	if err != nil {
		t.Fatalf("ValidateWithinVault: %v", err)
	}
	if !strings.HasPrefix(got, books) {
		t.Errorf("returned %q not under books", got)
	}
}

func TestValidateWithinVault_NullByte(t *testing.T) {
	books := setupBooks(t)
	_, err := ValidateWithinVault(books, "book\x00.md")
	if err == nil {
		t.Fatal("expected error for null byte in path")
	}
	if !strings.Contains(err.Error(), "null byte") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateWithinVault_Traversal(t *testing.T) {
	books := setupBooks(t)
	cases := []string{
		"../outside.md",
		"../../outside.md",
		"sub/../../../escape.md",
	}
	for _, rel := range cases {
		t.Run(rel, func(t *testing.T) {
			_, err := ValidateWithinVault(books, rel)
			if err == nil {
				t.Fatal("expected traversal error")
			}
			if !strings.Contains(err.Error(), "escape") {
				t.Errorf("expected escape error, got: %v", err)
			}
		})
	}
}

func TestValidateWithinVault_AbsoluteOutsideBooks(t *testing.T) {
	books := setupBooks(t)
	// A known outside path
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "foreign.md")
	_, err := ValidateWithinVault(books, outsideFile)
	if err == nil {
		t.Fatal("expected error for absolute path outside books")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("expected escape error, got: %v", err)
	}
}

func TestValidateWithinVault_ReservedWindowsNames(t *testing.T) {
	books := setupBooks(t)
	// Reserved names apply regardless of extension and case.
	cases := []string{
		"CON.md", "con.md", "Con.md",
		"PRN.md", "AUX.md", "NUL.md",
		"COM1.md", "com1.md", "COM9.md",
		"LPT1.md", "LPT9.md",
		"CON.txt.md",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateWithinVault(books, name)
			if err == nil {
				t.Fatalf("expected reserved-name error for %q", name)
			}
			if !strings.Contains(err.Error(), "reserved Windows name") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateWithinVault_ReservedNameInSubdir(t *testing.T) {
	books := setupBooks(t)
	parent := filepath.Join(books, "CON")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateWithinVault(books, "CON/book.md")
	if err == nil {
		t.Fatal("expected error for reserved-name directory component")
	}
}

func TestValidateWithinVault_NonReservedLookalike(t *testing.T) {
	// "CONTACT" or "COMET" shouldn't be mistaken for reserved names.
	books := setupBooks(t)
	for _, n := range []string{"CONTACT by Sagan.md", "COMET by Someone.md", "COM10.md"} {
		t.Run(n, func(t *testing.T) {
			if _, err := ValidateWithinVault(books, n); err != nil {
				t.Errorf("false positive for %q: %v", n, err)
			}
		})
	}
}

func TestValidateWithinVault_OverLength(t *testing.T) {
	books := setupBooks(t)
	// Build a filename so that the joined path exceeds MAX_PATH.
	leaf := strings.Repeat("a", maxPathRunes) + ".md"
	_, err := ValidateWithinVault(books, leaf)
	if err == nil {
		t.Fatal("expected over-length error")
	}
	if !strings.Contains(err.Error(), "MAX_PATH") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateWithinVault_BooksNotAbsolute(t *testing.T) {
	_, err := ValidateWithinVault("relative/books", "book.md")
	if err == nil {
		t.Fatal("expected error for non-absolute books path")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateWithinVault_SymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Creating symlinks on Windows requires admin or Developer Mode —
		// skip this test when that's not available. The POSIX path covers
		// the logic; Windows users typically cannot create symlinks in an
		// Obsidian vault anyway.
		t.Skip("Windows symlink creation requires Developer Mode/admin; skipping")
	}
	books := setupBooks(t)
	outside := t.TempDir()
	link := filepath.Join(books, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	// A file "inside" the symlink target is actually outside books.
	_, err := ValidateWithinVault(books, "escape-link/smuggled.md")
	if err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("expected escape error, got: %v", err)
	}
}

// setupGenericRoot creates a temp directory as an arbitrary root (not
// necessarily the Books folder) and returns its canonical absolute path.
// Mirrors setupBooks but decoupled from the Books-folder name.
func setupGenericRoot(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestValidateWithinRoot_Generic(t *testing.T) {
	root := setupGenericRoot(t, "backups")
	// The leaf file may not exist yet, but the parent directory must —
	// matching the vault-candidate case.
	if err := os.MkdirAll(filepath.Join(root, "books-20260417T103045Z"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := ValidateWithinRoot(root, "books-20260417T103045Z/Hyperion.md")
	if err != nil {
		t.Fatalf("ValidateWithinRoot: %v", err)
	}
	if !strings.HasPrefix(got, root) {
		t.Errorf("returned %q not under root %q", got, root)
	}
}

func TestValidateWithinRoot_TraversalRejected(t *testing.T) {
	root := setupGenericRoot(t, "backups")
	_, err := ValidateWithinRoot(root, "../escape.md")
	if err == nil {
		t.Fatal("expected traversal error")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("expected escape error, got: %v", err)
	}
}

func TestValidateWithinRoot_ReservedInComponent(t *testing.T) {
	root := setupGenericRoot(t, "backups")
	_, err := ValidateWithinRoot(root, "CON.md")
	if err == nil {
		t.Fatal("expected reserved-name rejection")
	}
	if !strings.Contains(err.Error(), "reserved Windows name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateWithinRoot_NonAbsoluteRoot(t *testing.T) {
	_, err := ValidateWithinRoot("relative/root", "file.md")
	if err == nil {
		t.Fatal("expected non-absolute root to be rejected")
	}
}

func TestIsReservedWindowsName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"CON", true},
		{"con", true}, // case-insensitive
		{"Con.md", true},
		{"CON.txt.md", true},
		{"PRN", true},
		{"AUX.md", true},
		{"NUL", true},
		{"COM1", true},
		{"COM9.md", true},
		{"LPT5", true},
		{"CONTACT", false},
		{"COMET", false},
		{"COM10", false},
		{"LPT", false},
		{"Hyperion", false},
		{"", false},
		{".md", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := isReservedWindowsName(c.in); got != c.want {
				t.Errorf("isReservedWindowsName(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
