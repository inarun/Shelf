package profile

import (
	"math"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/index/store"
)

func pi64(v int64) *int64     { return &v }
func pf64(v float64) *float64 { return &v }

func fixedNow() time.Time {
	return time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
}

func TestBuild_EmptyLibrary(t *testing.T) {
	p := Build(nil, fixedNow())
	if p == nil {
		t.Fatal("Build returned nil profile")
	}
	if p.BookCount != 0 || p.RatedCount != 0 {
		t.Errorf("empty profile not zeroed: %+v", p)
	}
	if p.RatingMean != 0 {
		t.Errorf("RatingMean = %v, want 0", p.RatingMean)
	}
	if p.TopAuthors != nil {
		t.Errorf("TopAuthors should be nil for empty input, got %v", p.TopAuthors)
	}
	if len(p.AxisMeans) != 0 {
		t.Errorf("AxisMeans should be empty map, got %v", p.AxisMeans)
	}
	if p.LengthStdev != nil {
		t.Errorf("LengthStdev should be nil, got %v", *p.LengthStdev)
	}
}

func TestBuild_UnratedBooksDoNotPollute(t *testing.T) {
	books := []store.BookRow{
		{Title: "Rated", Authors: []string{"Kay"}, Categories: []string{"sci-fi"},
			RatingOverall: pf64(5), RatingDimensions: map[string]int{"plot": 5},
			FinishedDates: []string{"2026-04-18"}},
		{Title: "Unrated", Authors: []string{"Who"}, Categories: []string{"fantasy"}},
	}
	p := Build(books, fixedNow())
	if p.BookCount != 2 || p.RatedCount != 1 {
		t.Errorf("counts: BookCount=%d RatedCount=%d want 2/1", p.BookCount, p.RatedCount)
	}
	if len(p.TopAuthors) != 1 || p.TopAuthors[0] != "Kay" {
		t.Errorf("TopAuthors = %v, want [Kay]", p.TopAuthors)
	}
	if len(p.TopShelves) != 1 || p.TopShelves[0] != "sci-fi" {
		t.Errorf("TopShelves = %v, want [sci-fi]", p.TopShelves)
	}
	if _, ok := p.AxisMeans["plot"]; !ok {
		t.Errorf("AxisMeans should have 'plot', got %v", p.AxisMeans)
	}
}

func TestBuild_RecencyDecayFavorsRecent(t *testing.T) {
	// Two equally rated books, one finished yesterday, one 2 years ago.
	// Different authors so TopAuthors can show the ordering.
	books := []store.BookRow{
		{Title: "Recent", Authors: []string{"Recent Ann"},
			RatingOverall: pf64(5), FinishedDates: []string{"2026-04-18"}},
		{Title: "Old", Authors: []string{"Old Ollie"},
			RatingOverall: pf64(5), FinishedDates: []string{"2024-04-18"}},
	}
	p := Build(books, fixedNow())
	if len(p.TopAuthors) != 2 {
		t.Fatalf("TopAuthors = %v, want 2", p.TopAuthors)
	}
	if p.TopAuthors[0] != "Recent Ann" {
		t.Errorf("TopAuthors ordering = %v, want recent author first", p.TopAuthors)
	}
}

func TestBuild_AxisStdevSuppressedForSingleSample(t *testing.T) {
	books := []store.BookRow{
		{RatingOverall: pf64(5), RatingDimensions: map[string]int{"plot": 5},
			FinishedDates: []string{"2026-04-18"}},
	}
	p := Build(books, fixedNow())
	if got, ok := p.AxisMeans["plot"]; !ok || got != 5 {
		t.Errorf("AxisMeans[plot] = %v ok=%v, want 5", got, ok)
	}
	if _, ok := p.AxisStdevs["plot"]; ok {
		t.Errorf("AxisStdevs[plot] should be absent for single sample")
	}
}

func TestBuild_AxisStdevReportedForMultipleSamples(t *testing.T) {
	books := []store.BookRow{
		{RatingOverall: pf64(5), RatingDimensions: map[string]int{"plot": 5},
			FinishedDates: []string{"2026-04-18"}},
		{RatingOverall: pf64(3), RatingDimensions: map[string]int{"plot": 3},
			FinishedDates: []string{"2026-04-17"}},
	}
	p := Build(books, fixedNow())
	if _, ok := p.AxisStdevs["plot"]; !ok {
		t.Errorf("AxisStdevs[plot] should be present for 2 samples")
	}
	// Values are 5 and 3 with nearly equal weights; population stdev is ~1.0.
	if got := p.AxisStdevs["plot"]; got < 0.9 || got > 1.1 {
		t.Errorf("AxisStdevs[plot] = %v, want ~1.0", got)
	}
}

func TestBuild_LengthStdevNilUntilTwoSamples(t *testing.T) {
	oneBook := []store.BookRow{
		{RatingOverall: pf64(5), TotalPages: pi64(400),
			FinishedDates: []string{"2026-04-18"}},
	}
	if p := Build(oneBook, fixedNow()); p.LengthStdev != nil {
		t.Errorf("single sample: LengthStdev should be nil, got %v", *p.LengthStdev)
	}

	twoBooks := []store.BookRow{
		{RatingOverall: pf64(5), TotalPages: pi64(400),
			FinishedDates: []string{"2026-04-18"}},
		{RatingOverall: pf64(4), TotalPages: pi64(200),
			FinishedDates: []string{"2026-04-17"}},
	}
	p := Build(twoBooks, fixedNow())
	if p.LengthStdev == nil {
		t.Fatal("two samples: LengthStdev should be non-nil")
	}
	if *p.LengthStdev <= 0 {
		t.Errorf("LengthStdev = %v, want > 0 for differing lengths", *p.LengthStdev)
	}
	if p.LengthMean < 250 || p.LengthMean > 350 {
		t.Errorf("LengthMean = %v, want roughly between 250 and 350", p.LengthMean)
	}
}

func TestBuild_SeriesInProgressHidesCompleted(t *testing.T) {
	books := []store.BookRow{
		// Complete series (1, 2) — should not appear in progress.
		{Title: "Done1", SeriesName: "ClosedArc", SeriesIndex: pf64(1)},
		{Title: "Done2", SeriesName: "ClosedArc", SeriesIndex: pf64(2)},
		// Gappy series (1, 3) — should appear.
		{Title: "Open1", SeriesName: "OpenArc", SeriesIndex: pf64(1)},
		{Title: "Open3", SeriesName: "OpenArc", SeriesIndex: pf64(3)},
	}
	p := Build(books, fixedNow())
	if len(p.SeriesInProgress) != 1 || p.SeriesInProgress[0] != "OpenArc" {
		t.Errorf("SeriesInProgress = %v, want [OpenArc]", p.SeriesInProgress)
	}
}

func TestBuild_UnparseableDateFallsBackToWeightOne(t *testing.T) {
	// Garbage Finished date; no Started. Book still contributes with
	// weight 1.0 so TopAuthors is populated.
	books := []store.BookRow{
		{Title: "Weird", Authors: []string{"Z"},
			RatingOverall: pf64(5), FinishedDates: []string{"not-a-date"}},
	}
	p := Build(books, fixedNow())
	if len(p.TopAuthors) != 1 || p.TopAuthors[0] != "Z" {
		t.Errorf("TopAuthors = %v, want [Z]", p.TopAuthors)
	}
	if p.RatedCount != 1 {
		t.Errorf("RatedCount = %d, want 1", p.RatedCount)
	}
}

func TestBuild_FutureDateClampsToNow(t *testing.T) {
	// Future finished date: Δt clamps to 0, weight stays 1.0.
	books := []store.BookRow{
		{Title: "TimeTraveler", Authors: []string{"F"},
			RatingOverall: pf64(5), FinishedDates: []string{"2030-01-01"}},
	}
	p := Build(books, fixedNow())
	// Rating mean = weighted mean of [5 with weight 1.0] = 5.
	if math.Abs(p.RatingMean-5) > 1e-9 {
		t.Errorf("RatingMean = %v, want 5", p.RatingMean)
	}
}

func TestBuild_StartedFallbackWhenNoFinish(t *testing.T) {
	// Only a started date: should still contribute (using recency weight
	// based on the started date, not weight=1).
	books := []store.BookRow{
		{Title: "InProgress", Authors: []string{"S"},
			RatingOverall: pf64(5), StartedDates: []string{"2026-04-01"}},
	}
	p := Build(books, fixedNow())
	if p.RatedCount != 1 {
		t.Errorf("RatedCount = %d, want 1", p.RatedCount)
	}
	if p.RatingMean < 4.9 || p.RatingMean > 5.1 {
		t.Errorf("RatingMean = %v, want close to 5", p.RatingMean)
	}
}

func TestBuild_TopNCapAtEight(t *testing.T) {
	// Construct 10 rated books with unique authors to verify TopN cap.
	var books []store.BookRow
	for i := 0; i < 10; i++ {
		books = append(books, store.BookRow{
			Authors:       []string{string(rune('A' + i))},
			RatingOverall: pf64(5),
			FinishedDates: []string{"2026-04-18"},
		})
	}
	p := Build(books, fixedNow())
	if len(p.TopAuthors) != topN {
		t.Errorf("TopAuthors len = %d, want %d", len(p.TopAuthors), topN)
	}
}

func TestBuild_TieBreakAlphabetical(t *testing.T) {
	// Two authors with equal score; alphabetical tiebreak.
	books := []store.BookRow{
		{Authors: []string{"Zed"}, RatingOverall: pf64(5),
			FinishedDates: []string{"2026-04-18"}},
		{Authors: []string{"Alice"}, RatingOverall: pf64(5),
			FinishedDates: []string{"2026-04-18"}},
	}
	p := Build(books, fixedNow())
	if len(p.TopAuthors) != 2 {
		t.Fatalf("TopAuthors = %v, want 2", p.TopAuthors)
	}
	if p.TopAuthors[0] != "Alice" {
		t.Errorf("alphabetical tiebreak broken: %v", p.TopAuthors)
	}
}

func TestRecencyWeight_HalflifeShape(t *testing.T) {
	now := fixedNow()
	cases := []struct {
		name   string
		book   store.BookRow
		wantLo float64
		wantHi float64
	}{
		{"today",
			store.BookRow{FinishedDates: []string{"2026-04-19"}},
			0.99, 1.00},
		{"one year ago",
			store.BookRow{FinishedDates: []string{"2025-04-19"}},
			0.49, 0.51},
		{"two years ago",
			store.BookRow{FinishedDates: []string{"2024-04-19"}},
			0.24, 0.26},
		{"no dates",
			store.BookRow{},
			1.00, 1.00},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := recencyWeight(c.book, now)
			if w < c.wantLo || w > c.wantHi {
				t.Errorf("recencyWeight = %v, want [%v, %v]", w, c.wantLo, c.wantHi)
			}
		})
	}
}

func TestWeightedMeanStdev_ZeroTotalWeight(t *testing.T) {
	zeroed := []weightedVal{{value: 5, weight: 0}, {value: 3, weight: 0}}
	if _, ok := weightedMean(zeroed); ok {
		t.Error("weightedMean should return ok=false when all weights are zero")
	}
	if _, ok := weightedStdev(zeroed, 4); ok {
		t.Error("weightedStdev should return ok=false when all weights are zero")
	}
}

func TestBuild_ShelfAxisMeansPopulatedForFrequentShelf(t *testing.T) {
	// Two books on "sci-fi" with plot ratings 5 and 4 — recency-weighted
	// mean should be ~4.5 because both finished on the same day so weights
	// match. ShelfAxisMeans["sci-fi"]["plot"] is the AxisMatch input the
	// S18 scorer reads.
	books := []store.BookRow{
		{Title: "A", Authors: []string{"X"}, Categories: []string{"sci-fi"},
			RatingOverall: pf64(5), RatingDimensions: map[string]int{"plot": 5},
			FinishedDates: []string{"2026-04-18"}},
		{Title: "B", Authors: []string{"Y"}, Categories: []string{"sci-fi"},
			RatingOverall: pf64(4), RatingDimensions: map[string]int{"plot": 4},
			FinishedDates: []string{"2026-04-18"}},
	}
	p := Build(books, fixedNow())
	got, ok := p.ShelfAxisMeans["sci-fi"]["plot"]
	if !ok {
		t.Fatalf("ShelfAxisMeans[sci-fi][plot] missing; got %+v", p.ShelfAxisMeans)
	}
	if math.Abs(got-4.5) > 1e-9 {
		t.Errorf("ShelfAxisMeans[sci-fi][plot] = %v, want ~4.5", got)
	}
}

func TestBuild_ShelfAxisMeansSuppressedForSingleSample(t *testing.T) {
	// Single (shelf, axis) sample → entry omitted. Mirrors the AxisStdevs
	// suppression rule so AxisMatch never reads from a one-book sample.
	books := []store.BookRow{
		{Title: "Only", Authors: []string{"Z"}, Categories: []string{"sci-fi"},
			RatingOverall: pf64(5), RatingDimensions: map[string]int{"plot": 5},
			FinishedDates: []string{"2026-04-18"}},
	}
	p := Build(books, fixedNow())
	if _, ok := p.ShelfAxisMeans["sci-fi"]["plot"]; ok {
		t.Errorf("expected (sci-fi, plot) suppressed for n=1, got %+v",
			p.ShelfAxisMeans)
	}
	if len(p.ShelfAxisMeans) != 0 {
		t.Errorf("expected empty ShelfAxisMeans, got %+v", p.ShelfAxisMeans)
	}
}
