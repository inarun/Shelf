package goodreads

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
)

func openResolverStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedBook(t *testing.T, s *store.Store, filename, title, author, isbn string) {
	t.Helper()
	_, err := s.UpsertBook(context.Background(), store.BookRow{
		Filename:      filename,
		CanonicalName: true,
		Title:         title,
		Authors:       []string{author},
		ISBN:          isbn,
		Status:        "unread",
		SizeBytes:     1,
		MtimeNanos:    1,
		IndexedAtUnix: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMatch_ISBN13Priority(t *testing.T) {
	s := openResolverStore(t)
	seedBook(t, s, "Hyperion by Dan Simmons.md", "Hyperion", "Dan Simmons", "9780553283686")

	rv, err := NewResolver(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	r := Record{Title: "Hyperion", Authors: []string{"Dan Simmons"}, ISBN13: "9780553283686"}
	res, ok := rv.Match(r)
	if !ok {
		t.Fatal("expected match")
	}
	if res.Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("Filename got %q", res.Filename)
	}
	if res.Reason != "ISBN13 match" {
		t.Errorf("Reason got %q", res.Reason)
	}
}

func TestMatch_ISBN10Fallback(t *testing.T) {
	s := openResolverStore(t)
	seedBook(t, s, "Hyperion by Dan Simmons.md", "Hyperion", "Dan Simmons", "0553283685")

	rv, _ := NewResolver(context.Background(), s)
	r := Record{Title: "Hyperion", Authors: []string{"Dan Simmons"}, ISBN10: "0553283685"}
	res, ok := rv.Match(r)
	if !ok {
		t.Fatal("expected match")
	}
	if res.Reason != "ISBN10 match" {
		t.Errorf("Reason got %q", res.Reason)
	}
}

func TestMatch_FuzzyAutoMatch(t *testing.T) {
	s := openResolverStore(t)
	// Same title up to case/punctuation; same surname.
	seedBook(t, s, "The Lord of the Rings by J.R.R. Tolkien.md",
		"The Lord of the Rings", "J.R.R. Tolkien", "")

	rv, _ := NewResolver(context.Background(), s)
	r := Record{Title: "The lord  of the rings", Authors: []string{"Tolkien"}}
	res, ok := rv.Match(r)
	if !ok {
		t.Fatal("expected fuzzy match")
	}
	if !res.Fuzzy {
		t.Error("expected Fuzzy=true")
	}
	if res.NeedsUserDecision {
		t.Error("should auto-match (ratio=1.00 after normalization, same surname)")
	}
}

func TestMatch_FuzzyConflictAuthorDiffers(t *testing.T) {
	s := openResolverStore(t)
	seedBook(t, s, "Hyperion by Dan Simmons.md", "Hyperion", "Dan Simmons", "")

	rv, _ := NewResolver(context.Background(), s)
	// Title identical; but author surname different.
	r := Record{Title: "Hyperion", Authors: []string{"Somebody Else"}}
	res, ok := rv.Match(r)
	if !ok {
		t.Fatal("expected match with NeedsUserDecision")
	}
	if !res.NeedsUserDecision {
		t.Error("expected NeedsUserDecision because surnames differ")
	}
}

func TestMatch_BelowSoftThresholdMisses(t *testing.T) {
	s := openResolverStore(t)
	seedBook(t, s, "Hyperion by Dan Simmons.md", "Hyperion", "Dan Simmons", "")

	rv, _ := NewResolver(context.Background(), s)
	r := Record{Title: "Completely Different Book", Authors: []string{"Nobody"}}
	_, ok := rv.Match(r)
	if ok {
		t.Error("expected no match below 0.80 threshold")
	}
}

func TestMatch_EmptyRecordMisses(t *testing.T) {
	s := openResolverStore(t)
	seedBook(t, s, "Hyperion by Dan Simmons.md", "Hyperion", "Dan Simmons", "")

	rv, _ := NewResolver(context.Background(), s)
	_, ok := rv.Match(Record{})
	if ok {
		t.Error("expected no match for empty record")
	}
}
