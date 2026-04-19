package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func fixture(name string) BookRow {
	ratingOverall := 4.0
	pages := int64(482)
	return BookRow{
		Filename:          name,
		CanonicalName:     true,
		Title:             "Hyperion",
		Subtitle:          "",
		Authors:           []string{"Dan Simmons"},
		Categories:        []string{"science-fiction", "space-opera"},
		Publisher:         "Doubleday",
		PublishDate:       "1989-05-26",
		TotalPages:        &pages,
		ISBN:              "9780385249492",
		Format:            "physical",
		Source:            "Library",
		RatingOverall:     &ratingOverall,
		RatingHasOverride: true,
		Status:            "finished",
		ReadCount:         1,
		StartedDates:      []string{"2025-03-09"},
		FinishedDates:     []string{"2025-04-02"},
		SizeBytes:         2048,
		MtimeNanos:        1713283200000000000,
		IndexedAtUnix:     1713283200,
	}
}

func TestOpen_RunsMigrations(t *testing.T) {
	s := openStore(t)
	// Fresh open — applying UpsertBook validates the schema is live.
	if _, err := s.UpsertBook(context.Background(), fixture("Hyperion by Dan Simmons.md")); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertBook_Insert(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	id, err := s.UpsertBook(ctx, fixture("Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("UpsertBook returned zero id")
	}
	got, err := s.GetBookByFilename(ctx, "Hyperion by Dan Simmons.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Hyperion" {
		t.Errorf("Title got %q", got.Title)
	}
	if len(got.Authors) != 1 || got.Authors[0] != "Dan Simmons" {
		t.Errorf("Authors got %v", got.Authors)
	}
	if len(got.Categories) != 2 {
		t.Errorf("Categories got %v", got.Categories)
	}
	if got.RatingOverall == nil || *got.RatingOverall != 4.0 {
		t.Errorf("RatingOverall got %v, want 4.0", got.RatingOverall)
	}
	if !got.RatingHasOverride {
		t.Errorf("RatingHasOverride got false, want true")
	}
	if len(got.RatingDimensions) != 0 {
		t.Errorf("RatingDimensions got %v, want empty", got.RatingDimensions)
	}
}

func TestUpsertBook_Update_RebuildsJoins(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	row := fixture("Hyperion by Dan Simmons.md")
	if _, err := s.UpsertBook(ctx, row); err != nil {
		t.Fatal(err)
	}

	// Change authors entirely — second upsert should rebuild the join
	// table so old co-authors disappear.
	row.Authors = []string{"Dan Simmons", "Co Author"}
	row.Categories = []string{"science-fiction"}
	if _, err := s.UpsertBook(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBookByFilename(ctx, row.Filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Authors) != 2 || got.Authors[0] != "Dan Simmons" || got.Authors[1] != "Co Author" {
		t.Errorf("Authors after update got %v", got.Authors)
	}
	if len(got.Categories) != 1 || got.Categories[0] != "science-fiction" {
		t.Errorf("Categories after update got %v", got.Categories)
	}
}

func TestUpsertBook_SeriesLinkage(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	first := fixture("The Way of Kings by Brandon Sanderson.md")
	first.Title = "The Way of Kings"
	first.Authors = []string{"Brandon Sanderson"}
	first.SeriesName = "The Stormlight Archive"
	idx1 := 1.0
	first.SeriesIndex = &idx1
	if _, err := s.UpsertBook(ctx, first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.Filename = "Words of Radiance by Brandon Sanderson.md"
	second.Title = "Words of Radiance"
	idx2 := 2.0
	second.SeriesIndex = &idx2
	if _, err := s.UpsertBook(ctx, second); err != nil {
		t.Fatal(err)
	}

	g1, err := s.GetBookByFilename(ctx, first.Filename)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := s.GetBookByFilename(ctx, second.Filename)
	if err != nil {
		t.Fatal(err)
	}
	if g1.SeriesID == nil || g2.SeriesID == nil {
		t.Fatal("both books should have SeriesID set")
	}
	if *g1.SeriesID != *g2.SeriesID {
		t.Errorf("expected shared series_id, got %d vs %d", *g1.SeriesID, *g2.SeriesID)
	}
	if g1.SeriesName != "The Stormlight Archive" {
		t.Errorf("SeriesName got %q", g1.SeriesName)
	}
}

func TestUpsertBook_CaseInsensitiveAuthor(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	first := fixture("A.md")
	first.Title = "A"
	first.Authors = []string{"Brandon Sanderson"}
	if _, err := s.UpsertBook(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := fixture("B.md")
	second.Title = "B"
	second.Authors = []string{"brandon sanderson"} // different case
	if _, err := s.UpsertBook(ctx, second); err != nil {
		t.Fatal(err)
	}

	// Both books share one author row.
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM authors").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 author row (NOCASE collation), got %d", n)
	}
}

func TestDeleteBook_CascadesJoins(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if _, err := s.UpsertBook(ctx, fixture("x.md")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBookByFilename(ctx, "x.md"); err != nil {
		t.Fatal(err)
	}
	var ba, bc int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM book_authors").Scan(&ba); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM book_categories").Scan(&bc); err != nil {
		t.Fatal(err)
	}
	if ba != 0 || bc != 0 {
		t.Errorf("join tables not cascaded: book_authors=%d book_categories=%d", ba, bc)
	}
	// authors and categories rows remain on purpose.
	var aTotal, cTotal int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM authors").Scan(&aTotal); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM categories").Scan(&cTotal); err != nil {
		t.Fatal(err)
	}
	if aTotal == 0 || cTotal == 0 {
		t.Errorf("authors/categories should survive book delete: a=%d c=%d", aTotal, cTotal)
	}
}

func TestDeleteBook_NotFound(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	err := s.DeleteBookByFilename(ctx, "does-not-exist.md")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListBooks_ByStatus(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	a := fixture("a.md")
	a.Status = "finished"
	a.Title = "A"
	b := fixture("b.md")
	b.Status = "reading"
	b.Title = "B"
	c := fixture("c.md")
	c.Status = "unread"
	c.Title = "C"
	for _, row := range []BookRow{a, b, c} {
		if _, err := s.UpsertBook(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListBooks(ctx, Filter{Status: "reading"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Filename != "b.md" {
		t.Errorf("ListBooks(status=reading) got %v", got)
	}
}

func TestListBooks_ByAuthorName(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	a := fixture("a.md")
	a.Title = "A"
	a.Authors = []string{"Brandon Sanderson"}
	b := fixture("b.md")
	b.Title = "B"
	b.Authors = []string{"Dan Simmons"}
	for _, row := range []BookRow{a, b} {
		if _, err := s.UpsertBook(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListBooks(ctx, Filter{AuthorName: "Brandon Sanderson"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Filename != "a.md" {
		t.Errorf("filter by author got %v", got)
	}
}

func TestListBooks_CanonicalOnly(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	canonical := fixture("canonical.md")
	canonical.Title = "C"
	nonCanonical := fixture("weird - filename.md")
	nonCanonical.Title = "N"
	nonCanonical.CanonicalName = false
	nonCanonical.Warnings = []string{"non-canonical filename"}
	for _, row := range []BookRow{canonical, nonCanonical} {
		if _, err := s.UpsertBook(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.ListBooks(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("unfiltered should return both, got %d", len(all))
	}
	canonicalOnly, err := s.ListBooks(ctx, Filter{CanonicalOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(canonicalOnly) != 1 || canonicalOnly[0].Filename != "canonical.md" {
		t.Errorf("canonical-only filter got %v", canonicalOnly)
	}
}

func TestAllFilenames_ReturnsStatPairs(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	a := fixture("a.md")
	a.SizeBytes = 111
	a.MtimeNanos = 111000
	b := fixture("b.md")
	b.SizeBytes = 222
	b.MtimeNanos = 222000
	for _, row := range []BookRow{a, b} {
		if _, err := s.UpsertBook(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.AllFilenames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(got))
	}
	if got["a.md"].SizeBytes != 111 || got["a.md"].MtimeNanos != 111000 {
		t.Errorf("a.md stat mismatch: %+v", got["a.md"])
	}
	if got["b.md"].SizeBytes != 222 || got["b.md"].MtimeNanos != 222000 {
		t.Errorf("b.md stat mismatch: %+v", got["b.md"])
	}
}

func TestGet_NotFound(t *testing.T) {
	s := openStore(t)
	_, err := s.GetBookByFilename(context.Background(), "missing.md")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetBookByISBN_Found(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if _, err := s.UpsertBook(ctx, fixture("Hyperion by Dan Simmons.md")); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetBookByISBN(ctx, "9780385249492")
	if err != nil {
		t.Fatalf("GetBookByISBN: %v", err)
	}
	if got.Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("Filename got %q", got.Filename)
	}
	if len(got.Authors) != 1 || got.Authors[0] != "Dan Simmons" {
		t.Errorf("Authors got %v", got.Authors)
	}
}

func TestGetBookByISBN_NotFound(t *testing.T) {
	s := openStore(t)
	_, err := s.GetBookByISBN(context.Background(), "9999999999999")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetBookByISBN_EmptyRejects(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	// A book with empty isbn should not match an empty-string query.
	row := fixture("Hyperion by Dan Simmons.md")
	row.ISBN = ""
	if _, err := s.UpsertBook(ctx, row); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetBookByISBN(ctx, "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for empty ISBN query, got %v", err)
	}
}

// TestUpsertBook_ConcurrentWriters guards the busy_timeout +
// SetMaxOpenConns(1) configuration in Open. Pre-fix, parallel writers
// would race for the WAL write lock and the loser would receive
// SQLITE_BUSY immediately. Post-fix, writers serialize at the Go pool
// layer (one open conn) and any residual contention is absorbed by the
// 5s busy_timeout. Mirrors the importer-vs-watcher contention pattern
// that produced the original 8 reindex errors during a Goodreads import.
func TestUpsertBook_ConcurrentWriters(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			row := fixture(fmt.Sprintf("book-%02d.md", i))
			row.Title = fmt.Sprintf("Book %02d", i)
			if _, err := s.UpsertBook(ctx, row); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent UpsertBook: %v", err)
	}

	got, err := s.AllFilenames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Errorf("expected %d rows after concurrent inserts, got %d", n, len(got))
	}
}

func TestGetBookByISBN_Ambiguous(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	// Two books sharing an ISBN — shouldn't happen in a healthy vault,
	// but the schema doesn't enforce uniqueness, so the store must tolerate
	// it and surface an unambiguous ErrAmbiguousISBN.
	a := fixture("A by Author.md")
	b := fixture("B by Author.md")
	a.ISBN = "9780000000001"
	b.ISBN = "9780000000001"
	if _, err := s.UpsertBook(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertBook(ctx, b); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetBookByISBN(ctx, "9780000000001")
	if !errors.Is(err, ErrAmbiguousISBN) {
		t.Errorf("expected ErrAmbiguousISBN, got %v", err)
	}
}
