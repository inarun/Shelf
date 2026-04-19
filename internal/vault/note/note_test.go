package note

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

const sampleFile = `---
title: Hyperion
authors:
  - Dan Simmons
rating: 3
status: reading
---
# Hyperion

Some body text.

## Notes

Original notes.
`

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// bumpMtime shifts a file's mtime forward so subsequent Stat calls see a
// different value regardless of filesystem resolution quirks.
func bumpMtime(t *testing.T, path string, delta time.Duration) {
	t.Helper()
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	newMtime := stat.ModTime().Add(delta)
	if err := os.Chtimes(path, newMtime, newMtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestRead_StampsSizeAndMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, _ := os.Stat(path)
	if n.Size != stat.Size() {
		t.Errorf("stamped size %d, file size %d", n.Size, stat.Size())
	}
	if n.MtimeNanos != stat.ModTime().UnixNano() {
		t.Errorf("stamped mtime differs from file mtime")
	}
	if n.Frontmatter.Title() != "Hyperion" {
		t.Errorf("Frontmatter title got %q", n.Frontmatter.Title())
	}
	if len(n.Body.Blocks) == 0 {
		t.Error("Body should have parsed blocks")
	}
}

func TestRead_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.md")
	writeFile(t, path, "# Just a heading\n")
	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if n.Frontmatter == nil {
		t.Error("Frontmatter should be non-nil (empty) for files without one")
	}
	if len(n.Frontmatter.MutatedKeys()) != 0 {
		t.Errorf("fresh empty frontmatter should have no mutated keys, got %v",
			n.Frontmatter.MutatedKeys())
	}
}

func TestSaveBody_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	over := 5.0
	if err := n.Frontmatter.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	n.Body.SetRatingFromFrontmatter(n.Frontmatter.Rating())
	if err := n.SaveBody(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("## Rating — ★ 5/5")) {
		t.Errorf("saved body missing managed rating section:\n%s", got)
	}
}

func TestSaveBody_RefusesOnStaleMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, path, 5*time.Second)

	err = n.SaveBody()
	if !errors.Is(err, ErrStale) {
		t.Errorf("expected ErrStale, got %v", err)
	}
}

func TestSaveBody_RefusesOnStaleSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}

	// External append increases size; Chtimes is not strictly needed,
	// but set the mtime back to n.MtimeNanos so size is the only diff.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nAppended.\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := os.Chtimes(path, time.Unix(0, n.MtimeNanos), time.Unix(0, n.MtimeNanos)); err != nil {
		t.Fatal(err)
	}

	err = n.SaveBody()
	if !errors.Is(err, ErrStale) {
		t.Errorf("expected ErrStale on size mismatch, got %v", err)
	}
}

func TestSaveBody_AtomicReplace_NoTempLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	over := 5.0
	if err := n.Frontmatter.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	n.Body.SetRatingFromFrontmatter(n.Frontmatter.Rating())
	if err := n.SaveBody(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".shelf.tmp") {
			t.Errorf("atomic write left temp file: %s", e.Name())
		}
	}
}

func TestSaveFrontmatter_PreservesConcurrentBodyEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}

	// External body edit: replace the Notes section text.
	externallyEdited := strings.Replace(sampleFile,
		"Original notes.", "EXTERNALLY EDITED.", 1)
	writeFile(t, path, externallyEdited)
	bumpMtime(t, path, 2*time.Second)

	// App-side frontmatter mutation: rating change.
	over := 5.0
	if err := n.Frontmatter.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	if err := n.SaveFrontmatter(); err != nil {
		t.Fatalf("SaveFrontmatter should succeed unconditionally: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("EXTERNALLY EDITED.")) {
		t.Errorf("external body edit lost:\n%s", got)
	}
	if !bytes.Contains(got, []byte("overall: 5")) {
		t.Errorf("frontmatter rating not applied:\n%s", got)
	}
	// Original scalar rating line must be gone.
	if bytes.Contains(got, []byte("rating: 3")) {
		t.Errorf("old scalar rating still present:\n%s", got)
	}
}

func TestSaveFrontmatter_UnconditionalOnStaleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	// Make the file stale from the Note's perspective.
	writeFile(t, path, sampleFile+"\n# extra heading\n")
	bumpMtime(t, path, 3*time.Second)

	// Mutate frontmatter; SaveFrontmatter must NOT refuse with ErrStale.
	over := 4.0
	if err := n.Frontmatter.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	if err := n.SaveFrontmatter(); err != nil {
		t.Fatalf("SaveFrontmatter should be unconditional: %v", err)
	}

	got, _ := os.ReadFile(path)
	if !bytes.Contains(got, []byte("overall: 4")) {
		t.Errorf("new rating not applied:\n%s", got)
	}
}

func TestSaveFrontmatter_AtomicReplace_NoTempLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	over := 5.0
	if err := n.Frontmatter.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	if err := n.SaveFrontmatter(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".shelf.tmp") {
			t.Errorf("atomic write left temp file: %s", e.Name())
		}
	}
}

func TestSaveFrontmatter_DoesNotOverwriteUntouchedFields(t *testing.T) {
	// Touches only the rating setter. Reader sees the file; someone
	// externally renames title in the frontmatter; SaveFrontmatter
	// should apply only `rating` and leave the external title change.
	dir := t.TempDir()
	path := filepath.Join(dir, "book.md")
	writeFile(t, path, sampleFile)

	n, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}

	externallyEdited := strings.Replace(sampleFile,
		"title: Hyperion", "title: Fall of Hyperion", 1)
	writeFile(t, path, externallyEdited)
	bumpMtime(t, path, 2*time.Second)

	over := 5.0
	if err := n.Frontmatter.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	if err := n.SaveFrontmatter(); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if !bytes.Contains(got, []byte("title: Fall of Hyperion")) {
		t.Errorf("external title edit clobbered:\n%s", got)
	}
	if !bytes.Contains(got, []byte("overall: 5")) {
		t.Errorf("rating not applied:\n%s", got)
	}
}
