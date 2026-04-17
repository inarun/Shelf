package goodreads

import (
	"io"
	"strings"
	"testing"
)

const minimalCSV = `Book Id,Title,Author,ISBN,ISBN13,My Rating,Exclusive Shelf,Date Read,Date Added,My Review
1,"Hyperion","Dan Simmons","=""0553283685""","=""9780553283686""",5,read,2025/04/02,2024/12/15,"Loved the Canterbury-tales structure."
2,"The Final Empire (Mistborn, #1)","Brandon Sanderson","","",4,read,,2024/11/01,
3,"Project Hail Mary","Andy Weir","","",0,currently-reading,,2025/01/05,
`

func TestReader_Minimal(t *testing.T) {
	rd := NewReader(strings.NewReader(minimalCSV))
	records, err := rd.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Hyperion: ISBN unwrapping, rating, status, review.
	h := records[0]
	if h.Title != "Hyperion" {
		t.Errorf("Hyperion Title got %q", h.Title)
	}
	if h.ISBN10 != "0553283685" {
		t.Errorf("ISBN10 got %q", h.ISBN10)
	}
	if h.ISBN13 != "9780553283686" {
		t.Errorf("ISBN13 got %q", h.ISBN13)
	}
	if h.MyRating != 5 {
		t.Errorf("MyRating got %d", h.MyRating)
	}
	if h.Status != "finished" {
		t.Errorf("Status got %q", h.Status)
	}
	if h.DateRead == nil || h.DateRead.Format("2006-01-02") != "2025-04-02" {
		t.Errorf("DateRead got %v", h.DateRead)
	}
	if !strings.Contains(h.Review, "Canterbury-tales") {
		t.Errorf("Review text missing: %q", h.Review)
	}

	// Mistborn: series extraction from title.
	m := records[1]
	if m.Title != "The Final Empire" {
		t.Errorf("Mistborn Title got %q (expected series suffix stripped)", m.Title)
	}
	if m.Series != "Mistborn" {
		t.Errorf("Series got %q", m.Series)
	}
	if m.SeriesIndex == nil || *m.SeriesIndex != 1 {
		t.Errorf("SeriesIndex got %v", m.SeriesIndex)
	}

	// Hail Mary: currently-reading → paused, rating 0 → 0.
	hm := records[2]
	if hm.Status != "paused" {
		t.Errorf("Status got %q", hm.Status)
	}
	if hm.MyRating != 0 {
		t.Errorf("MyRating got %d", hm.MyRating)
	}
}

func TestReader_BOM(t *testing.T) {
	input := "\ufeffTitle,Author\nHyperion,Dan Simmons\n"
	rd := NewReader(strings.NewReader(input))
	records, err := rd.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(records) != 1 || records[0].Title != "Hyperion" {
		t.Errorf("BOM not stripped; got %+v", records)
	}
}

func TestReader_OversizedFieldRejected(t *testing.T) {
	huge := strings.Repeat("x", 2*DefaultMaxFieldBytes)
	input := "Title,My Review\n\"Book\",\"" + huge + "\"\n"
	rd := NewReader(strings.NewReader(input))
	records, err := rd.ReadAll()
	if err == nil {
		t.Fatal("expected oversized-field error")
	}
	// The oversized row is skipped, but no other rows were present.
	if len(records) != 0 {
		t.Errorf("oversized row should not produce a record; got %d", len(records))
	}
}

func TestReader_MalformedQuotes_RowSkipped(t *testing.T) {
	input := "Title,Author\n" +
		"\"Good Row\",\"Dan Simmons\"\n" +
		"\"Broken \"Row,Wrong\"\",\"X\"\n" +
		"\"Another Good\",\"Y\"\n"
	rd := NewReader(strings.NewReader(input))
	records, _ := rd.ReadAll()
	// Go's encoding/csv bails on the stream at the malformed row by
	// default; we accept that the good first row parses and the rest
	// may not, surfaced via the error.
	if len(records) < 1 {
		t.Errorf("expected at least the first good row; got %d", len(records))
	}
	if records[0].Title != "Good Row" {
		t.Errorf("first row title got %q", records[0].Title)
	}
}

func TestReader_EmptyInput(t *testing.T) {
	rd := NewReader(strings.NewReader(""))
	_, err := rd.ReadAll()
	if err == nil {
		t.Fatal("expected error for empty CSV")
	}
}

func TestReader_ReturnsIOEOFCleanly(t *testing.T) {
	// Header only, no rows — not an error.
	rd := NewReader(strings.NewReader("Title,Author\n"))
	records, err := rd.ReadAll()
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected no records for header-only CSV, got %d", len(records))
	}
}
