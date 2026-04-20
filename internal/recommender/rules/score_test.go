package rules

import (
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/domain/series"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

func TestRank_OrderingByCombinedScore(t *testing.T) {
	// Three candidates: one matches author + series-gap (high), one
	// matches genre only (mid), one matches nothing (zero). Combined
	// score must put them in that order.
	stdev := 100.0
	p := &profile.Profile{
		TopAuthors:  []string{"Le Guin"},
		TopShelves:  []string{"sci-fi"},
		LengthMean:  300, LengthStdev: &stdev,
	}
	ss := []series.State{{Name: "Earthsea", Total: 5, Gaps: []int{2}}}
	cands := []store.BookRow{
		{Filename: "C.md", Title: "Empty", Authors: []string{"Other"}, Categories: []string{"history"}},
		{Filename: "A.md", Title: "High", Authors: []string{"Le Guin"},
			SeriesName: "Earthsea", SeriesIndex: pf(2), Categories: []string{"sci-fi"}},
		{Filename: "B.md", Title: "Mid", Authors: []string{"Other"}, Categories: []string{"sci-fi"}},
	}
	out := Rank(cands, p, ss, DefaultWeights)
	if len(out) != 3 {
		t.Fatalf("got %d ranked, want 3", len(out))
	}
	if out[0].Title != "High" || out[1].Title != "Mid" || out[2].Title != "Empty" {
		t.Errorf("ordering = [%s, %s, %s], want [High, Mid, Empty]",
			out[0].Title, out[1].Title, out[2].Title)
	}
}

func TestRank_TopThreeReasonsByContribution(t *testing.T) {
	// Trigger 5 scorers; assert exactly 3 reasons surfaced and ordered
	// by Value*Weight descending.
	stdev := 50.0
	p := &profile.Profile{
		TopAuthors:  []string{"Le Guin"},
		TopShelves:  []string{"sci-fi"},
		LengthMean:  300, LengthStdev: &stdev,
		ShelfAxisMeans: map[string]map[string]float64{"sci-fi": {"plot": 4.8}},
	}
	ss := []series.State{{Name: "Earthsea", Total: 5, Gaps: []int{2}}}
	b := store.BookRow{
		Filename: "X.md", Authors: []string{"Le Guin"},
		SeriesName: "Earthsea", SeriesIndex: pf(2),
		Categories: []string{"sci-fi"},
		TotalPages: pi(300),
	}
	out := Rank([]store.BookRow{b}, p, ss, DefaultWeights)
	if len(out) != 1 {
		t.Fatalf("got %d ranked, want 1", len(out))
	}
	if got := len(out[0].Reasons); got != 3 {
		t.Errorf("len(Reasons) = %d, want 3", got)
	}
	// SeriesCompletion has weight 1.5 + value 1.0 = 1.5 — strongest.
	if !strings.Contains(out[0].Reasons[0], "Continues") {
		t.Errorf("Reasons[0] = %q, want SeriesCompletion to lead", out[0].Reasons[0])
	}
}

func TestRank_EmptyCandidatesReturnsEmpty(t *testing.T) {
	out := Rank(nil, &profile.Profile{}, nil, DefaultWeights)
	if out == nil {
		t.Fatal("Rank returned nil; want non-nil empty slice")
	}
	if len(out) != 0 {
		t.Errorf("len(out) = %d, want 0", len(out))
	}
}

func TestRank_ZeroScoreCandidateStillIncluded(t *testing.T) {
	// Fresh-user profile (everything empty/nil) → all scorers return 0.
	// Candidates must still be returned with Score: 0, Reasons: [].
	cands := []store.BookRow{{Filename: "A.md", Title: "Solo"}}
	out := Rank(cands, &profile.Profile{}, nil, DefaultWeights)
	if len(out) != 1 {
		t.Fatalf("got %d ranked, want 1", len(out))
	}
	if out[0].Score != 0 {
		t.Errorf("Score = %v, want 0", out[0].Score)
	}
	if out[0].Reasons == nil {
		t.Errorf("Reasons should be non-nil empty slice for JSON []")
	}
	if len(out[0].Reasons) != 0 {
		t.Errorf("len(Reasons) = %d, want 0", len(out[0].Reasons))
	}
}

func TestRank_DeterministicTiebreak(t *testing.T) {
	// Two candidates with identical (zero) scores → alpha by filename.
	cands := []store.BookRow{
		{Filename: "Z.md", Title: "Zed"},
		{Filename: "A.md", Title: "Aey"},
	}
	out := Rank(cands, &profile.Profile{}, nil, DefaultWeights)
	if out[0].Filename != "A.md" {
		t.Errorf("ordering[0] = %s, want A.md (alpha tiebreak)", out[0].Filename)
	}
}

