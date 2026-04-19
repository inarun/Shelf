// Integration tests for Session 2's vault round-trip. These stand apart
// from unit tests in internal/* so they can drive the full stack end-
// to-end on a synthetic vault. They must never touch the user's real
// vault — every test uses t.TempDir for its books folder.
package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/index/store"
	sync_ "github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/watcher"
)

const hyperion = `---
title: Hyperion
authors:
  - Dan Simmons
categories:
  - science-fiction
rating: 3
status: finished
started:
  - 2025-03-09
finished:
  - 2025-04-02
read_count: 1
---
# Hyperion

Rating — 3/5

## Notes

Dense first half.

## Reading Timeline

- 2025-03-09 — Started
- 2025-04-02 — Finished
`

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func setup(t *testing.T) (string, *store.Store, *sync_.Syncer) {
	t.Helper()
	root := t.TempDir()
	books := filepath.Join(root, "books")
	if err := os.MkdirAll(books, 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(root, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return books, s, sync_.New(s, books)
}

func TestRoundTrip_ReadMutateWriteIsByteStable(t *testing.T) {
	books, _, _ := setup(t)
	path := filepath.Join(books, "Hyperion by Dan Simmons.md")
	writeFile(t, path, hyperion)

	n, err := note.Read(path)
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

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The body must be byte-identical; the frontmatter rating region
	// switches shape (scalar → map) so we check key properties instead
	// of demanding exactly one changed line.
	if !strings.Contains(string(got), "overall: 5") {
		t.Errorf("frontmatter didn't pick up new rating:\n%s", got)
	}
	if strings.Contains(string(got), "rating: 3") {
		t.Errorf("old scalar rating still present:\n%s", got)
	}
	// Body untouched — the legacy H1 "Rating — 3/5" line still there
	// because we didn't mutate the body.
	if !strings.Contains(string(got), "Rating — 3/5") {
		t.Errorf("body modified unexpectedly:\n%s", got)
	}
	if !bytes.Contains(got, []byte("## Reading Timeline")) {
		t.Errorf("body lost after SaveFrontmatter:\n%s", got)
	}
}

func TestIndexRebuild_DeterministicFromVault(t *testing.T) {
	books, s, sy := setup(t)
	writeFile(t, filepath.Join(books, "Hyperion by Dan Simmons.md"), hyperion)
	writeFile(t, filepath.Join(books, "Dune by Frank Herbert.md"),
		"---\ntitle: Dune\nauthors:\n  - Frank Herbert\nstatus: reading\n---\n# Dune\n")

	ctx := context.Background()
	if _, err := sy.FullScan(ctx); err != nil {
		t.Fatal(err)
	}
	firstState, err := captureIndexState(ctx, s)
	if err != nil {
		t.Fatal(err)
	}

	// Nuke and rebuild from scratch.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(filepath.Dir(books), "index.db")
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	// Also remove the WAL/SHM sidecars that SQLite may leave behind.
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")

	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	sy2 := sync_.New(s2, books)
	if _, err := sy2.FullScan(ctx); err != nil {
		t.Fatal(err)
	}
	secondState, err := captureIndexState(ctx, s2)
	if err != nil {
		t.Fatal(err)
	}
	if firstState != secondState {
		t.Errorf("rebuild not deterministic:\nfirst:\n%s\nsecond:\n%s", firstState, secondState)
	}
}

// captureIndexState returns a stable string representation of the
// index's content (ignoring indexed_at_unix which varies between runs).
func captureIndexState(ctx context.Context, s *store.Store) (string, error) {
	books, err := s.ListBooks(ctx, store.Filter{})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, b := range books {
		sb.WriteString(b.Filename)
		sb.WriteString("|")
		sb.WriteString(b.Title)
		sb.WriteString("|")
		sb.WriteString(strings.Join(b.Authors, ","))
		sb.WriteString("|")
		sb.WriteString(strings.Join(b.Categories, ","))
		sb.WriteString("|")
		sb.WriteString(b.Status)
		sb.WriteString("|")
		if b.RatingOverall != nil {
			sb.WriteString(strconv.FormatFloat(*b.RatingOverall, 'f', 1, 64))
		}
		sb.WriteString("|")
		sb.WriteString(strings.Join(b.StartedDates, ","))
		sb.WriteString("|")
		sb.WriteString(strings.Join(b.FinishedDates, ","))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func TestConcurrentBodyEdit_RefusesSave(t *testing.T) {
	books, _, _ := setup(t)
	path := filepath.Join(books, "book.md")
	writeFile(t, path, hyperion)

	n, err := note.Read(path)
	if err != nil {
		t.Fatal(err)
	}

	// External edit shifts mtime; guaranteed non-equal via Chtimes.
	writeFile(t, path, hyperion+"\nappended\n")
	future := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	over := 5.0
	if err := n.Frontmatter.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	n.Body.SetRatingFromFrontmatter(n.Frontmatter.Rating())
	err = n.SaveBody()
	if !errors.Is(err, note.ErrStale) {
		t.Errorf("expected ErrStale, got %v", err)
	}
}

func TestConcurrentBodyEdit_FrontmatterWriteStillSucceeds(t *testing.T) {
	books, _, _ := setup(t)
	path := filepath.Join(books, "book.md")
	writeFile(t, path, hyperion)

	n, err := note.Read(path)
	if err != nil {
		t.Fatal(err)
	}

	externallyEdited := strings.Replace(hyperion,
		"Dense first half.", "EXTERNALLY EDITED NOTES.", 1)
	writeFile(t, path, externallyEdited)
	future := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	over := 5.0
	if err := n.Frontmatter.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
		t.Fatal(err)
	}
	if err := n.SaveFrontmatter(); err != nil {
		t.Fatalf("SaveFrontmatter should be unconditional: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Contains(got, []byte("EXTERNALLY EDITED NOTES.")) {
		t.Errorf("external body edit clobbered:\n%s", got)
	}
	if !bytes.Contains(got, []byte("overall: 5")) {
		t.Errorf("app rating not applied:\n%s", got)
	}
}

func TestWatcher_DrivesSync(t *testing.T) {
	books, s, sy := setup(t)
	w, err := watcher.New(books)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// Write a book — watcher must emit KindCreate, sync must index it.
	path := filepath.Join(books, "Hyperion by Dan Simmons.md")
	writeFile(t, path, hyperion)

	ctx := context.Background()
	select {
	case ev := <-w.Events():
		if ev.Kind != watcher.KindCreate {
			t.Fatalf("expected KindCreate, got %v", ev.Kind)
		}
		if err := sy.Apply(ctx, sync_.Event{Kind: sync_.EventCreate, Path: ev.Path}); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for watcher event")
	}

	if _, err := s.GetBookByFilename(ctx, "Hyperion by Dan Simmons.md"); err != nil {
		t.Errorf("book not indexed after watcher-driven sync: %v", err)
	}
}

