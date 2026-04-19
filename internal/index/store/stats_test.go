package store

import (
	"context"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "shelf.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedBook(t *testing.T, s *Store, fn func(*BookRow)) {
	t.Helper()
	base := BookRow{
		Filename:      "Placeholder.md",
		CanonicalName: true,
		Title:         "Placeholder",
		Status:        "unread",
		SizeBytes:     100,
		MtimeNanos:    1,
		IndexedAtUnix: 1,
	}
	fn(&base)
	if _, err := s.UpsertBook(context.Background(), base); err != nil {
		t.Fatalf("UpsertBook: %v", err)
	}
}

func TestStats_Counts(t *testing.T) {
	s := testStore(t)
	two := 2.0
	four := 4.0
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "A.md"
		b.Title = "A"
		b.Status = "finished"
		b.RatingOverall = &four
		b.RatingHasOverride = true
		b.ReadCount = 2
	})
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "B.md"
		b.Title = "B"
		b.Status = "reading"
		b.RatingOverall = &two
		b.RatingHasOverride = true
		b.ReadCount = 0
	})
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "C.md"
		b.Title = "C"
		b.Status = "unread"
	})

	sm, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if sm.TotalBooks != 3 {
		t.Errorf("TotalBooks: %d", sm.TotalBooks)
	}
	if sm.StatusCounts["finished"] != 1 || sm.StatusCounts["reading"] != 1 || sm.StatusCounts["unread"] != 1 {
		t.Errorf("StatusCounts: %v", sm.StatusCounts)
	}
	if sm.TotalReads != 2 {
		t.Errorf("TotalReads: %d", sm.TotalReads)
	}
	if sm.RatedBooks != 2 {
		t.Errorf("RatedBooks: %d", sm.RatedBooks)
	}
	if sm.AverageRating != 3.0 {
		t.Errorf("AverageRating: %v", sm.AverageRating)
	}
}

func TestStats_Empty(t *testing.T) {
	s := testStore(t)
	sm, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if sm.TotalBooks != 0 || sm.TotalReads != 0 || sm.RatedBooks != 0 || sm.AverageRating != 0 {
		t.Errorf("non-zero stats on empty store: %+v", sm)
	}
}

func TestBooksPerYear(t *testing.T) {
	s := testStore(t)
	pages := int64(300)
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "A.md"
		b.Title = "A"
		b.Status = "finished"
		b.TotalPages = &pages
		b.FinishedDates = []string{"2024-01-10"}
	})
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "B.md"
		b.Title = "B"
		b.Status = "finished"
		b.TotalPages = &pages
		b.FinishedDates = []string{"2024-11-15", "2025-03-20"}
	})
	// Book with no total_pages — contributes to Books count but 0 pages.
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "C.md"
		b.Title = "C"
		b.Status = "finished"
		b.FinishedDates = []string{"2025-06-01"}
	})
	// Book with malformed date — should be skipped.
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "D.md"
		b.Title = "D"
		b.Status = "finished"
		b.FinishedDates = []string{"bad-date"}
	})

	years, err := s.BooksPerYear(context.Background())
	if err != nil {
		t.Fatalf("BooksPerYear: %v", err)
	}
	if len(years) != 2 {
		t.Fatalf("expected 2 years, got %d: %+v", len(years), years)
	}
	if years[0].Year != "2024" || years[0].Books != 2 || years[0].Pages != 600 {
		t.Errorf("2024: %+v", years[0])
	}
	if years[1].Year != "2025" || years[1].Books != 2 || years[1].Pages != 300 {
		t.Errorf("2025: %+v", years[1])
	}
}

func TestListSeries_ExcludesEmpty(t *testing.T) {
	s := testStore(t)
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "Dune.md"
		b.Title = "Dune"
		b.SeriesName = "Dune"
		b.Status = "finished"
	})
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "Dune Messiah.md"
		b.Title = "Dune Messiah"
		b.SeriesName = "Dune"
		b.Status = "unread"
	})
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "Empire.md"
		b.Title = "The Final Empire"
		b.SeriesName = "Mistborn"
		b.Status = "finished"
	})

	out, err := s.ListSeries(context.Background())
	if err != nil {
		t.Fatalf("ListSeries: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 series, got %d: %+v", len(out), out)
	}
	// Ordered case-insensitively by name.
	if out[0].Name != "Dune" || out[0].BookCount != 2 || out[0].Finished != 1 {
		t.Errorf("Dune: %+v", out[0])
	}
	if out[1].Name != "Mistborn" || out[1].BookCount != 1 || out[1].Finished != 1 {
		t.Errorf("Mistborn: %+v", out[1])
	}
}

func TestGetSeriesByName_OrdersByIndex(t *testing.T) {
	s := testStore(t)
	one := 1.0
	two := 2.0
	oneHalf := 1.5
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "b.md"
		b.Title = "Second"
		b.SeriesName = "Cycle"
		b.SeriesIndex = &two
	})
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "a.md"
		b.Title = "First"
		b.SeriesName = "Cycle"
		b.SeriesIndex = &one
	})
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "c.md"
		b.Title = "Zero (no index)"
		b.SeriesName = "Cycle"
	})
	seedBook(t, s, func(b *BookRow) {
		b.Filename = "d.md"
		b.Title = "One-Point-Five"
		b.SeriesName = "Cycle"
		b.SeriesIndex = &oneHalf
	})

	d, err := s.GetSeriesByName(context.Background(), "cycle")
	if err != nil {
		t.Fatalf("GetSeriesByName: %v", err)
	}
	if d.Name != "Cycle" || len(d.Books) != 4 {
		t.Fatalf("summary: %+v books=%d", d.SeriesSummary, len(d.Books))
	}
	// Expected order: 1, 1.5, 2, null-index (which lands last).
	wantTitles := []string{"First", "One-Point-Five", "Second", "Zero (no index)"}
	for i, b := range d.Books {
		if b.Title != wantTitles[i] {
			t.Errorf("pos %d: got %q want %q", i, b.Title, wantTitles[i])
		}
	}
}

func TestGetSeriesByName_NotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetSeriesByName(context.Background(), "Never heard of")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
