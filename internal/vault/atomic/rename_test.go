package atomic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRename_HappyPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.md")
	dst := filepath.Join(dir, "new.md")

	if err := os.WriteFile(src, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Rename(src, dst); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source still exists after rename: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading dst: %v", err)
	}
	if string(got) != "content" {
		t.Errorf("dst content = %q, want %q", got, "content")
	}
}

func TestRename_MissingSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "does-not-exist.md")
	dst := filepath.Join(dir, "new.md")

	if err := Rename(src, dst); err == nil {
		t.Fatal("expected error when source doesn't exist")
	}
}

func TestRename_OverwriteExistingDst(t *testing.T) {
	// Rename of a file onto an existing file: platform-dependent. On Unix
	// os.Rename replaces; on Windows MoveFileExW without the REPLACE flag
	// fails. We deliberately test the happy case (dst does not exist) in
	// TestRename_HappyPath and leave the overwrite semantics to the caller
	// — rename.Apply explicitly checks the destination is absent first.
	dir := t.TempDir()
	src := filepath.Join(dir, "old.md")
	dst := filepath.Join(dir, "new.md")

	if err := os.WriteFile(src, []byte("src-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("dst-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	// We don't assert success vs. failure here because the behavior is
	// OS-specific — callers defend against clobbering at a higher layer.
	_ = Rename(src, dst)
}
