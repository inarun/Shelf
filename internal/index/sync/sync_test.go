package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
)

const fixtureHyperion = `---
tag: 📚Book
title: Hyperion
authors:
  - Dan Simmons
categories:
  - science-fiction
rating: 4
status: finished
started:
  - 2025-03-09
finished:
  - 2025-04-02
read_count: 1
---
# Hyperion

Body text.

## Notes

Dense.
`

const fixtureDune = `---
title: Dune
authors:
  - Frank Herbert
status: reading
---
# Dune
`

const fixtureSeries = `---
title: The Way of Kings
authors:
  - Brandon Sanderson
series: The Stormlight Archive
series_index: 1
status: unread
---
# The Way of Kings
`

func setupVault(t *testing.T) (string, *store.Store, *Syncer) {
	t.Helper()
	dir := t.TempDir()
	booksDir := filepath.Join(dir, "books")
	if err := os.MkdirAll(booksDir, 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dir, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return booksDir, s, New(s, booksDir)
}

func writeBook(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFullScan_IndexesAllMdFiles(t *testing.T) {
	dir, s, sy := setupVault(t)
	writeBook(t, dir, "Hyperion by Dan Simmons.md", fixtureHyperion)
	writeBook(t, dir, "Dune by Frank Herbert.md", fixtureDune)
	writeBook(t, dir, "The Way of Kings by Brandon Sanderson.md", fixtureSeries)

	rep, err := sy.FullScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 3 || rep.Indexed != 3 {
		t.Errorf("expected scanned=3 indexed=3, got %+v", rep)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("unexpected errors: %+v", rep.Errors)
	}
	books, err := s.ListBooks(context.Background(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 3 {
		t.Errorf("expected 3 books in index, got %d", len(books))
	}
}

func TestFullScan_IgnoresNonMdAndTempFiles(t *testing.T) {
	dir, _, sy := setupVault(t)
	writeBook(t, dir, "Dune by Frank Herbert.md", fixtureDune)
	writeBook(t, dir, "cover.png", "binary")
	writeBook(t, dir, "README.txt", "notes")
	// Shelf atomic-write temp file: leading dot, 16 hex chars, .shelf.tmp
	writeBook(t, dir,
		".Dune by Frank Herbert.md.abcdef0123456789.shelf.tmp",
		"interrupted write")

	rep, err := sy.FullScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 1 {
		t.Errorf("scanned got %d, want 1", rep.Scanned)
	}
}

func TestFullScan_FlagsNonCanonicalFilename(t *testing.T) {
	dir, s, sy := setupVault(t)
	writeBook(t, dir, "My Book - John Doe.md", fixtureDune)

	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBookByFilename(context.Background(), "My Book - John Doe.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalName {
		t.Errorf("expected canonical_name=false, got true")
	}
	if len(got.Warnings) == 0 {
		t.Errorf("expected non-canonical warning, got none")
	}
}

func TestFullScan_DeletesVanishedRows(t *testing.T) {
	dir, s, sy := setupVault(t)
	path := writeBook(t, dir, "Dune by Frank Herbert.md", fixtureDune)

	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	rep, err := sy.FullScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Deleted != 1 {
		t.Errorf("expected deleted=1, got %+v", rep)
	}
	if _, err := s.GetBookByFilename(context.Background(), "Dune by Frank Herbert.md"); err == nil {
		t.Error("expected row to be gone after vanish scan")
	}
}

func TestFullScan_SkipsUnchangedFiles(t *testing.T) {
	dir, _, sy := setupVault(t)
	writeBook(t, dir, "Dune by Frank Herbert.md", fixtureDune)

	first, err := sy.FullScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Indexed != 1 {
		t.Fatalf("first scan should index, got %+v", first)
	}
	second, err := sy.FullScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Indexed != 0 || second.Skipped != 1 {
		t.Errorf("unchanged file should be skipped, got %+v", second)
	}
}

func TestFullScan_SurvivesMalformedFrontmatter(t *testing.T) {
	dir, s, sy := setupVault(t)
	writeBook(t, dir, "good.md", fixtureDune)
	writeBook(t, dir, "bad.md", "---\n[not yaml\n---\n")

	rep, err := sy.FullScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Errors) == 0 {
		t.Errorf("expected at least one FileError for bad.md")
	}
	if rep.Indexed != 1 {
		t.Errorf("good file should still index, got %+v", rep)
	}
	if _, err := s.GetBookByFilename(context.Background(), "good.md"); err != nil {
		t.Errorf("good file missing from index: %v", err)
	}
}

func TestApply_Write_ReindexesChangedFile(t *testing.T) {
	dir, s, sy := setupVault(t)
	path := writeBook(t, dir, "Dune by Frank Herbert.md", fixtureDune)
	ctx := context.Background()
	if err := sy.Apply(ctx, Event{Kind: EventCreate, Path: path}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBookByFilename(ctx, "Dune by Frank Herbert.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "reading" {
		t.Errorf("initial status got %q", got.Status)
	}
	// Edit on disk and re-apply.
	updated := `---
title: Dune
authors:
  - Frank Herbert
status: finished
rating: 5
---
# Dune
`
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sy.Apply(ctx, Event{Kind: EventWrite, Path: path}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetBookByFilename(ctx, "Dune by Frank Herbert.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "finished" {
		t.Errorf("status after write got %q", got.Status)
	}
	if got.RatingOverall == nil || *got.RatingOverall != 5.0 {
		t.Errorf("rating after write got %v, want 5.0", got.RatingOverall)
	}
	if !got.RatingHasOverride {
		t.Errorf("RatingHasOverride got false; legacy scalar should carry override flag")
	}
}

func TestApply_Remove_DeletesRow(t *testing.T) {
	dir, s, sy := setupVault(t)
	path := writeBook(t, dir, "Dune by Frank Herbert.md", fixtureDune)
	ctx := context.Background()
	if err := sy.Apply(ctx, Event{Kind: EventCreate, Path: path}); err != nil {
		t.Fatal(err)
	}
	if err := sy.Apply(ctx, Event{Kind: EventRemove, Path: path}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBookByFilename(ctx, "Dune by Frank Herbert.md"); err == nil {
		t.Error("expected row to be gone after EventRemove")
	}
}

func TestApply_Rename_UpdatesFilename(t *testing.T) {
	dir, s, sy := setupVault(t)
	oldPath := writeBook(t, dir, "Dune by Frank Herbert.md", fixtureDune)
	ctx := context.Background()
	if err := sy.Apply(ctx, Event{Kind: EventCreate, Path: oldPath}); err != nil {
		t.Fatal(err)
	}

	newPath := filepath.Join(dir, "Dune by F. Herbert.md")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if err := sy.Apply(ctx, Event{Kind: EventRename, Path: newPath, OldPath: oldPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBookByFilename(ctx, "Dune by Frank Herbert.md"); err == nil {
		t.Error("old filename still in index after rename")
	}
	if _, err := s.GetBookByFilename(ctx, "Dune by F. Herbert.md"); err != nil {
		t.Errorf("new filename missing from index after rename: %v", err)
	}
}

func TestApply_IgnoresNonMd(t *testing.T) {
	dir, _, sy := setupVault(t)
	path := writeBook(t, dir, "cover.png", "x")
	err := sy.Apply(context.Background(), Event{Kind: EventCreate, Path: path})
	if err != nil {
		t.Errorf("non-.md Apply should be no-op, got %v", err)
	}
}

func TestIsShelfTempFile(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{".book.md.0123456789abcdef.shelf.tmp", true},
		{".book.md.ABCDEF0123456789.shelf.tmp", false}, // uppercase not emitted
		{".book.md.short.shelf.tmp", false},            // not 16 hex
		{"book.md.0123456789abcdef.shelf.tmp", false},  // missing leading dot
		{".book.md.0123456789abcdef.shelf.tmp.md", false},
		{".book.0123456789abcdef.shelf.tmp", true}, // no .md in base, still matches
		{"book.md", false},
	}
	for _, c := range cases {
		if got := isShelfTempFile(c.in); got != c.want {
			t.Errorf("isShelfTempFile(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
