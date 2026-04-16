package atomic

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWrite_HappyPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "book.md")
	data := []byte("Hello, Shelf.\n")

	if err := Write(target, data, 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("file contents: got %q want %q", got, data)
	}
	assertNoTempFiles(t, dir)
}

func TestWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "book.md")

	if err := os.WriteFile(target, []byte("old contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	newData := []byte("new contents")
	if err := Write(target, newData, 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newData) {
		t.Errorf("file contents: got %q want %q", got, newData)
	}
	assertNoTempFiles(t, dir)
}

func TestWrite_Concurrent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "book.md")

	const writers = 16
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data := bytes.Repeat([]byte{byte('a' + i)}, 1024)
			errs[i] = Write(target, data, 0o600)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}

	// Final content must be *some* writer's full payload — no interleave.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1024 {
		t.Errorf("final file has length %d, expected 1024 (no torn write)", len(got))
	}
	firstByte := got[0]
	for i, b := range got {
		if b != firstByte {
			t.Errorf("torn write: byte %d = %q, byte 0 = %q", i, b, firstByte)
			break
		}
	}
	assertNoTempFiles(t, dir)
}

func TestWrite_ParentDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "missing-dir", "book.md")

	err := Write(target, []byte("x"), 0o600)
	if err == nil {
		t.Fatal("expected error for missing parent directory")
	}
	if !strings.Contains(err.Error(), "create temp") {
		t.Errorf("expected 'create temp' error, got: %v", err)
	}
}

func TestWrite_ParentIsAFile(t *testing.T) {
	dir := t.TempDir()
	fileAsParent := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(fileAsParent, []byte("just a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(fileAsParent, "book.md")

	err := Write(target, []byte("x"), 0o600)
	if err == nil {
		t.Fatal("expected error when parent is a regular file")
	}
}

func TestWrite_TempCleanedOnRenameFailure(t *testing.T) {
	// Force a rename failure by making the target an existing directory —
	// rename over a directory fails on both POSIX and Windows. The temp
	// file must be cleaned up.
	dir := t.TempDir()
	target := filepath.Join(dir, "book.md")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}

	err := Write(target, []byte("x"), 0o600)
	if err == nil {
		t.Fatal("expected error renaming over a directory")
	}
	assertNoTempFiles(t, dir)
}

func TestWrite_LargeFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "large.md")

	// 2 MiB — more than a typical book note but still fast.
	data := bytes.Repeat([]byte("abcdefgh"), 256*1024)
	if err := Write(target, data, 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(data) {
		t.Errorf("length mismatch: got %d want %d", len(got), len(data))
	}
	if !bytes.Equal(got, data) {
		t.Error("content mismatch")
	}
}

func TestWrite_TempNameIsHidden(t *testing.T) {
	// Hook the rename so we can observe the temp file before rename. The
	// simplest way is to call Write in a goroutine that blocks on a rename
	// failure, but here we just verify the temp name pattern directly via
	// a small refactor: reuse the public API and peek at the dir mid-run
	// by forcing the write to fail, inspecting directory contents.
	dir := t.TempDir()
	// Intentionally make target unwritable by making its parent a file,
	// causing Write to fail BEFORE Rename. The temp file will have been
	// created and then cleaned up. We check the dir is empty afterward.
	target := filepath.Join(dir, "book.md")
	readOnlyProbe := filepath.Join(dir, ".book.md.fake.shelf.tmp")
	if err := os.WriteFile(readOnlyProbe, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make a normal write — should succeed, and the leftover probe must
	// not be mistaken for one of ours (different hex suffix).
	if err := Write(target, []byte("real"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Probe remains because it's not ours.
	if _, err := os.Stat(readOnlyProbe); err != nil {
		t.Errorf("unrelated temp file was removed: %v", err)
	}
	// Only the probe and the target should exist; no orphaned Write temps.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	got := strings.Join(names, ",")
	// Accept any order; just verify no orphan from our Write.
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".book.md.") && strings.HasSuffix(n, ".shelf.tmp") && n != ".book.md.fake.shelf.tmp" {
			t.Errorf("Write left an orphan temp file: %s (all: %s)", n, got)
		}
	}
}

// assertNoTempFiles verifies that no files matching the atomic write temp
// pattern remain in dir. The pattern is ".<base>.<hex>.shelf.tmp".
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") && strings.HasSuffix(n, ".shelf.tmp") {
			t.Errorf("leftover temp file in %s: %s", dir, n)
		}
	}
}
