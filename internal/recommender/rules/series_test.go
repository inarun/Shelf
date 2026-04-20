package rules

import (
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/domain/series"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

func pf(v float64) *float64 { return &v }

func TestScoreSeriesCompletion_GapHit(t *testing.T) {
	b := store.BookRow{SeriesName: "Hyperion Cantos", SeriesIndex: pf(2)}
	ss := []series.State{{Name: "Hyperion Cantos", Total: 4, Gaps: []int{2, 4}}}
	s := ScoreSeriesCompletion(b, &profile.Profile{}, ss)
	if s.Value != 1.0 {
		t.Errorf("Value = %v, want 1.0", s.Value)
	}
	if !strings.Contains(s.Reason, "Hyperion Cantos") || !strings.Contains(s.Reason, "2 of 4") {
		t.Errorf("Reason = %q", s.Reason)
	}
}

func TestScoreSeriesCompletion_NotInGap(t *testing.T) {
	b := store.BookRow{SeriesName: "X", SeriesIndex: pf(5)}
	ss := []series.State{{Name: "X", Total: 5, Gaps: []int{2}}}
	s := ScoreSeriesCompletion(b, &profile.Profile{}, ss)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score, got %+v", s)
	}
}

func TestScoreSeriesCompletion_NilSeriesIndex(t *testing.T) {
	b := store.BookRow{SeriesName: "X"}
	ss := []series.State{{Name: "X", Total: 3, Gaps: []int{1, 2, 3}}}
	s := ScoreSeriesCompletion(b, &profile.Profile{}, ss)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score for nil index, got %+v", s)
	}
}

func TestScoreSeriesCompletion_EmptySeriesName(t *testing.T) {
	b := store.BookRow{SeriesIndex: pf(1)}
	ss := []series.State{{Name: "X", Total: 1, Gaps: []int{1}}}
	s := ScoreSeriesCompletion(b, &profile.Profile{}, ss)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score for empty series name, got %+v", s)
	}
}

func TestScoreSeriesCompletion_FractionalIndexFloors(t *testing.T) {
	// Index 2.5 in a series with Gap=2 → floor(2.5)=2 matches.
	b := store.BookRow{SeriesName: "X", SeriesIndex: pf(2.5)}
	ss := []series.State{{Name: "X", Total: 3, Gaps: []int{2}}}
	s := ScoreSeriesCompletion(b, &profile.Profile{}, ss)
	if s.Value != 1.0 {
		t.Errorf("Value = %v, want 1.0 (floor of 2.5 is 2 which is in Gaps)", s.Value)
	}
}
