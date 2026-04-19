package series

import (
	"reflect"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
)

func f64(v float64) *float64 { return &v }

func book(series string, idx *float64) store.BookRow {
	return store.BookRow{SeriesName: series, SeriesIndex: idx}
}

func TestDetect_EmptyInput(t *testing.T) {
	if out := Detect(nil); out != nil {
		t.Errorf("nil input: expected nil, got %+v", out)
	}
	if out := Detect([]store.BookRow{}); out != nil {
		t.Errorf("empty input: expected nil, got %+v", out)
	}
}

func TestDetect_NoSeriesAtAll(t *testing.T) {
	in := []store.BookRow{
		{Title: "Orphan 1"},
		{Title: "Orphan 2"},
	}
	if out := Detect(in); out != nil {
		t.Errorf("standalone books: expected nil, got %+v", out)
	}
}

func TestDetect_CompleteRun(t *testing.T) {
	in := []store.BookRow{
		book("Hyperion Cantos", f64(1)),
		book("Hyperion Cantos", f64(2)),
		book("Hyperion Cantos", f64(3)),
		book("Hyperion Cantos", f64(4)),
	}
	got := Detect(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 state, got %d", len(got))
	}
	s := got[0]
	if !s.Complete {
		t.Errorf("expected Complete=true, got %+v", s)
	}
	if s.Total != 4 || s.Owned != 4 || s.MaxOwnedIndex != 4 {
		t.Errorf("totals wrong: %+v", s)
	}
	if len(s.Gaps) != 0 {
		t.Errorf("expected no gaps, got %v", s.Gaps)
	}
}

func TestDetect_GapReported(t *testing.T) {
	in := []store.BookRow{
		book("Mistborn", f64(1)),
		book("Mistborn", f64(3)),
	}
	got := Detect(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 state, got %d", len(got))
	}
	s := got[0]
	if s.Complete {
		t.Errorf("expected Complete=false, got %+v", s)
	}
	if s.Total != 3 {
		t.Errorf("Total = %d, want 3", s.Total)
	}
	if !reflect.DeepEqual(s.Gaps, []int{2}) {
		t.Errorf("Gaps = %v, want [2]", s.Gaps)
	}
}

func TestDetect_FractionalIndexDoesNotContributeToTotal(t *testing.T) {
	in := []store.BookRow{
		book("Mistborn", f64(1)),
		book("Mistborn", f64(1.5)), // novella
		book("Mistborn", f64(2)),
	}
	got := Detect(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 state, got %d", len(got))
	}
	s := got[0]
	if s.Total != 2 {
		t.Errorf("Total = %d, want 2 (1.5 floors to 1, does not raise Total)", s.Total)
	}
	if s.Owned != 3 {
		t.Errorf("Owned = %d, want 3 (novella counts)", s.Owned)
	}
	if s.MaxOwnedIndex != 2 {
		t.Errorf("MaxOwnedIndex = %v, want 2", s.MaxOwnedIndex)
	}
	if !s.Complete {
		t.Errorf("expected Complete=true (integers 1..2 covered), got %+v", s)
	}
}

func TestDetect_NilIndexCountsOwnedOnly(t *testing.T) {
	in := []store.BookRow{
		book("Hyperion Cantos", f64(1)),
		book("Hyperion Cantos", nil), // user hasn't set series_index yet
	}
	got := Detect(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 state, got %d", len(got))
	}
	s := got[0]
	if s.Owned != 2 {
		t.Errorf("Owned = %d, want 2", s.Owned)
	}
	if s.Total != 1 {
		t.Errorf("Total = %d, want 1 (only the indexed book contributes)", s.Total)
	}
	if !s.Complete {
		t.Errorf("expected Complete=true since integer 1 is covered; got %+v", s)
	}
}

func TestDetect_OnlyNilIndicesStaysIncomplete(t *testing.T) {
	in := []store.BookRow{
		book("Some Trilogy", nil),
		book("Some Trilogy", nil),
	}
	got := Detect(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 state, got %d", len(got))
	}
	s := got[0]
	if s.Total != 0 {
		t.Errorf("Total = %d, want 0 (no indices observed)", s.Total)
	}
	if s.Complete {
		t.Errorf("Complete should be false when Total == 0: %+v", s)
	}
	if len(s.Gaps) != 0 {
		t.Errorf("Gaps should be empty when Total == 0, got %v", s.Gaps)
	}
}

func TestDetect_ZeroOrNegativeIndicesIgnored(t *testing.T) {
	// Integer indices < 1 make no sense in series numbering; we treat them
	// like fractionals that don't move Total.
	in := []store.BookRow{
		book("Oddities", f64(0)),
		book("Oddities", f64(-2)),
		book("Oddities", f64(1)),
	}
	got := Detect(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 state, got %d", len(got))
	}
	s := got[0]
	if s.Total != 1 {
		t.Errorf("Total = %d, want 1", s.Total)
	}
	if !s.Complete {
		t.Errorf("expected Complete=true; got %+v", s)
	}
}

func TestDetect_MultiSeriesSortedByName(t *testing.T) {
	in := []store.BookRow{
		book("Zorlak Chronicles", f64(1)),
		book("Alice Stories", f64(1)),
		book("Alice Stories", f64(2)),
		book("Middle March", f64(1)),
	}
	got := Detect(in)
	if len(got) != 3 {
		t.Fatalf("expected 3 states, got %d", len(got))
	}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	want := []string{"Alice Stories", "Middle March", "Zorlak Chronicles"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("sort order = %v, want %v", names, want)
	}
}

func TestDetect_GapsAlwaysAscending(t *testing.T) {
	in := []store.BookRow{
		book("Gappy", f64(5)),
		book("Gappy", f64(1)),
	}
	got := Detect(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 state, got %d", len(got))
	}
	if !reflect.DeepEqual(got[0].Gaps, []int{2, 3, 4}) {
		t.Errorf("Gaps = %v, want [2 3 4]", got[0].Gaps)
	}
}
