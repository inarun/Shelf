package goodreads

import "testing"

func TestNormalize_ISBNExcelFormula(t *testing.T) {
	raw := map[string]string{"isbn": `="0553283685"`, "isbn13": `="9780553283686"`}
	r, _ := Normalize(raw, 1)
	if r.ISBN10 != "0553283685" {
		t.Errorf("ISBN10 got %q", r.ISBN10)
	}
	if r.ISBN13 != "9780553283686" {
		t.Errorf("ISBN13 got %q", r.ISBN13)
	}
}

func TestNormalize_ISBNInvalidChecksum(t *testing.T) {
	raw := map[string]string{"isbn": "1234567890", "isbn13": "1234567890123"}
	r, _ := Normalize(raw, 1)
	if r.ISBN10 != "" {
		t.Errorf("invalid ISBN10 should be cleared; got %q", r.ISBN10)
	}
	if r.ISBN13 != "" {
		t.Errorf("invalid ISBN13 should be cleared; got %q", r.ISBN13)
	}
}

func TestNormalize_SeriesExtraction(t *testing.T) {
	raw := map[string]string{"title": "The Final Empire (Mistborn, #1)", "author": "Brandon Sanderson"}
	r, _ := Normalize(raw, 1)
	if r.Title != "The Final Empire" {
		t.Errorf("Title got %q", r.Title)
	}
	if r.Series != "Mistborn" {
		t.Errorf("Series got %q", r.Series)
	}
	if r.SeriesIndex == nil || *r.SeriesIndex != 1 {
		t.Errorf("SeriesIndex got %v", r.SeriesIndex)
	}
}

func TestNormalize_SeriesFractionalIndex(t *testing.T) {
	raw := map[string]string{"title": "The Emperor's Soul (Elantris, #1.5)", "author": "Brandon Sanderson"}
	r, _ := Normalize(raw, 1)
	if r.SeriesIndex == nil || *r.SeriesIndex != 1.5 {
		t.Errorf("SeriesIndex got %v", r.SeriesIndex)
	}
}

func TestNormalize_TitleWithoutSeries(t *testing.T) {
	raw := map[string]string{"title": "Hyperion", "author": "Dan Simmons"}
	r, _ := Normalize(raw, 1)
	if r.Title != "Hyperion" {
		t.Errorf("Title got %q", r.Title)
	}
	if r.Series != "" {
		t.Errorf("Series should be empty; got %q", r.Series)
	}
}

func TestNormalize_SlashDate(t *testing.T) {
	raw := map[string]string{"date read": "2025/04/02", "date added": "2024-12-15"}
	r, errs := Normalize(raw, 1)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if r.DateRead == nil || r.DateRead.Format("2006-01-02") != "2025-04-02" {
		t.Errorf("DateRead got %v", r.DateRead)
	}
	if r.DateAdded == nil || r.DateAdded.Format("2006-01-02") != "2024-12-15" {
		t.Errorf("DateAdded got %v", r.DateAdded)
	}
}

func TestNormalize_InvalidDate(t *testing.T) {
	raw := map[string]string{"date read": "not a date"}
	r, errs := Normalize(raw, 1)
	if len(errs) == 0 {
		t.Error("expected error for invalid date")
	}
	if r.DateRead != nil {
		t.Errorf("DateRead should be nil on parse failure; got %v", r.DateRead)
	}
}

func TestNormalize_BookshelvesFilterExclusive(t *testing.T) {
	raw := map[string]string{"bookshelves": "sci-fi,favorites,to-read,currently-reading,read"}
	r, _ := Normalize(raw, 1)
	want := []string{"sci-fi", "favorites"}
	if len(r.Bookshelves) != len(want) {
		t.Fatalf("Bookshelves got %v, want %v", r.Bookshelves, want)
	}
	for i, w := range want {
		if r.Bookshelves[i] != w {
			t.Errorf("Bookshelves[%d] = %q, want %q", i, r.Bookshelves[i], w)
		}
	}
}

func TestNormalize_StatusMapping(t *testing.T) {
	cases := []struct {
		shelf, want string
	}{
		{"to-read", "unread"},
		{"currently-reading", "paused"},
		{"read", "finished"},
		{"unknown", ""},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.shelf, func(t *testing.T) {
			raw := map[string]string{"exclusive shelf": c.shelf}
			r, _ := Normalize(raw, 1)
			if r.Status != c.want {
				t.Errorf("Status(%q) got %q, want %q", c.shelf, r.Status, c.want)
			}
		})
	}
}

func TestNormalize_AdditionalAuthors(t *testing.T) {
	raw := map[string]string{
		"author":              "Neil Gaiman",
		"additional authors":  "Terry Pratchett, Pratchett Jr.",
	}
	r, _ := Normalize(raw, 1)
	if len(r.Authors) != 3 {
		t.Fatalf("Authors got %v", r.Authors)
	}
	if r.Authors[0] != "Neil Gaiman" || r.Authors[1] != "Terry Pratchett" || r.Authors[2] != "Pratchett Jr." {
		t.Errorf("Authors order got %v", r.Authors)
	}
}

func TestNormalize_RatingOutOfRange(t *testing.T) {
	raw := map[string]string{"my rating": "7"}
	_, errs := Normalize(raw, 1)
	if len(errs) == 0 {
		t.Error("expected error for rating out of range")
	}
}

func TestISBN10_Valid(t *testing.T) {
	if !isbn10Valid("0553283685") {
		t.Error("Hyperion ISBN10 should be valid")
	}
	if isbn10Valid("0553283686") {
		t.Error("wrong checksum should be rejected")
	}
	if isbn10Valid("") {
		t.Error("empty should be rejected")
	}
	if isbn10Valid("123456789") {
		t.Error("wrong length should be rejected")
	}
}

func TestISBN13_Valid(t *testing.T) {
	if !isbn13Valid("9780553283686") {
		t.Error("Hyperion ISBN13 should be valid")
	}
	if isbn13Valid("9780553283687") {
		t.Error("wrong checksum should be rejected")
	}
	if isbn13Valid("1234567890") {
		t.Error("wrong length should be rejected")
	}
}
