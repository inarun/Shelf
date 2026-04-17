package note

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

func buildNote(t *testing.T, title string) (*frontmatter.Frontmatter, *body.Body) {
	t.Helper()
	fm := frontmatter.NewEmpty()
	fm.SetTitle(title)
	fm.SetAuthors([]string{"Test Author"})
	bd := &body.Body{}
	bd.SetTitle(title)
	return fm, bd
}

func TestCreate_WritesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Hyperion by Dan Simmons.md")
	fm, bd := buildNote(t, "Hyperion")

	if err := Create(path, fm, bd); err != nil {
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "title: Hyperion") {
		t.Errorf("frontmatter not written; got:\n%s", data)
	}
	if !strings.Contains(string(data), "# Hyperion") {
		t.Errorf("body not written; got:\n%s", data)
	}
}

func TestCreate_RefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.md")
	if err := os.WriteFile(path, []byte("existing content"), 0o600); err != nil {
		t.Fatal(err)
	}
	fm, bd := buildNote(t, "New")

	err := Create(path, fm, bd)
	if err == nil {
		t.Fatal("expected error for existing file")
	}
	if !errors.Is(err, fs.ErrExist) {
		t.Errorf("expected fs.ErrExist, got %v", err)
	}
	// Original content should survive.
	got, _ := os.ReadFile(path)
	if string(got) != "existing content" {
		t.Errorf("existing file was overwritten: %q", got)
	}
}

func TestCreate_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	fm, bd := buildNote(t, "T")
	if err := Create(path, fm, bd); err != nil {
		t.Fatalf("Create: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".shelf.tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestCreate_UsesUserOnlyPerm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	fm, bd := buildNote(t, "T")
	if err := Create(path, fm, bd); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// On Windows the permission bits are limited; assert at least that the
	// file is not world-readable on Unix. On Windows Mode().Perm() is
	// consistently 0o666 for regular files, so we only check Unix.
	perm := stat.Mode().Perm()
	if perm&0o077 != 0 && !isWindowsSkipPerm() {
		t.Errorf("file perm %v is world/group-accessible; expected 0o600", perm)
	}
}
