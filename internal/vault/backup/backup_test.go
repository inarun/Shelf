package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func setupVault(t *testing.T) (booksDir, backupsRoot string) {
	t.Helper()
	dir := t.TempDir()
	books := filepath.Join(dir, "Books")
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(books, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	return books, backups
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshot_CopiesEveryFile_Recursive(t *testing.T) {
	books, backups := setupVault(t)
	writeFile(t, filepath.Join(books, "Hyperion.md"), "# Hyperion")
	writeFile(t, filepath.Join(books, "covers", "hyperion.jpg"), "image bytes")
	writeFile(t, filepath.Join(books, "drafts", "Unfinished Review.md"), "scratch")
	writeFile(t, filepath.Join(books, "notes.txt"), "misc notes")

	info, err := Snapshot(context.Background(), books, backups)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if info.Root == "" {
		t.Fatal("empty Root")
	}
	if info.Bytes == 0 {
		t.Error("Bytes should be > 0")
	}

	// Check each expected file appears under Root.
	want := []string{
		filepath.Join("Hyperion.md"),
		filepath.Join("covers", "hyperion.jpg"),
		filepath.Join("drafts", "Unfinished Review.md"),
		filepath.Join("notes.txt"),
	}
	for _, rel := range want {
		p := filepath.Join(info.Root, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %q in backup: %v", rel, err)
		}
	}
	// Info.Files matches.
	sort.Strings(info.Files)
	sort.Strings(want)
	for i, w := range want {
		if i >= len(info.Files) {
			t.Fatalf("missing %q in manifest (have %v)", w, info.Files)
		}
		if info.Files[i] != w {
			t.Errorf("manifest[%d] = %q, want %q", i, info.Files[i], w)
		}
	}
}

func TestSnapshot_EmptyBooksDir(t *testing.T) {
	books, backups := setupVault(t)
	info, err := Snapshot(context.Background(), books, backups)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := os.Stat(info.Root); err != nil {
		t.Errorf("backup dir should exist: %v", err)
	}
	if len(info.Files) != 0 {
		t.Errorf("expected no files, got %v", info.Files)
	}
	if info.Bytes != 0 {
		t.Errorf("expected 0 bytes, got %d", info.Bytes)
	}
}

func TestSnapshot_DestinationUnderSourceRejected(t *testing.T) {
	dir := t.TempDir()
	books := filepath.Join(dir, "Books")
	if err := os.MkdirAll(books, 0o700); err != nil {
		t.Fatal(err)
	}
	// backups root inside books — classic misconfiguration.
	backups := filepath.Join(books, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := Snapshot(context.Background(), books, backups)
	if !errors.Is(err, ErrDestinationUnderSource) {
		t.Errorf("expected ErrDestinationUnderSource, got %v", err)
	}
}

func TestSnapshot_CollisionAppendsSuffix(t *testing.T) {
	books, backups := setupVault(t)
	writeFile(t, filepath.Join(books, "a.md"), "content")

	// Pre-create the backup dir for the current second, forcing Snapshot
	// to pick the -01 suffix. Don't rely on timing — the test would flake
	// if two Snapshot calls ever landed in different seconds.
	stamp := time.Now().UTC().Format("20060102T150405Z")
	if err := os.Mkdir(filepath.Join(backups, "books-"+stamp), 0o700); err != nil {
		t.Fatal(err)
	}

	info, err := Snapshot(context.Background(), books, backups)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Suffix should be a two-digit number or the plain stamp again; assert
	// it's a new directory different from the reserved one.
	if filepath.Base(info.Root) == "books-"+stamp {
		t.Errorf("should have chosen a different directory; got %s", info.Root)
	}
	if !strings.Contains(filepath.Base(info.Root), "books-"+stamp) {
		t.Errorf("backup dir should contain stamp; got %s", info.Root)
	}
}

func TestSnapshot_ContextCancellation(t *testing.T) {
	books, backups := setupVault(t)
	// Many small files so cancellation catches mid-walk.
	for i := 0; i < 50; i++ {
		writeFile(t, filepath.Join(books, "file", "f", "x.md"), "content")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Snapshot(ctx, books, backups)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSnapshot_SubdirPreserved(t *testing.T) {
	books, backups := setupVault(t)
	writeFile(t, filepath.Join(books, "a", "b", "deep.md"), "deep content")

	info, err := Snapshot(context.Background(), books, backups)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	p := filepath.Join(info.Root, "a", "b", "deep.md")
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading deep file: %v", err)
	}
	if string(got) != "deep content" {
		t.Errorf("deep content mismatch: %q", got)
	}
}

func TestSnapshot_NonRegularFilesIgnored(t *testing.T) {
	books, backups := setupVault(t)
	writeFile(t, filepath.Join(books, "real.md"), "real")
	// Make a subdirectory — should be traversed, not copied as a file.
	if err := os.MkdirAll(filepath.Join(books, "emptysubdir"), 0o700); err != nil {
		t.Fatal(err)
	}

	info, err := Snapshot(context.Background(), books, backups)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(info.Files) != 1 || info.Files[0] != "real.md" {
		t.Errorf("expected only real.md, got %v", info.Files)
	}
}

func TestSnapshot_BytesAccounting(t *testing.T) {
	books, backups := setupVault(t)
	writeFile(t, filepath.Join(books, "a.md"), "12345")
	writeFile(t, filepath.Join(books, "b.md"), "ABC")

	info, err := Snapshot(context.Background(), books, backups)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if info.Bytes != 8 {
		t.Errorf("Bytes got %d, want 8", info.Bytes)
	}
}
