package rules

import (
	"math"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

func TestScoreAxisMatch_HighShelfAxisHit(t *testing.T) {
	b := store.BookRow{Categories: []string{"sci-fi"}}
	p := &profile.Profile{
		ShelfAxisMeans: map[string]map[string]float64{
			"sci-fi": {"plot": 4.8},
		},
	}
	s := ScoreAxisMatch(b, p)
	want := (4.8 - axisValueFloor) / 2
	if math.Abs(s.Value-want) > 1e-9 {
		t.Errorf("Value = %v, want %v", s.Value, want)
	}
	if !strings.Contains(s.Reason, "Plot") || !strings.Contains(s.Reason, "sci-fi") {
		t.Errorf("Reason = %q, want it to mention Plot and sci-fi", s.Reason)
	}
}

func TestScoreAxisMatch_BelowThresholdNoEmit(t *testing.T) {
	b := store.BookRow{Categories: []string{"sci-fi"}}
	p := &profile.Profile{
		ShelfAxisMeans: map[string]map[string]float64{
			"sci-fi": {"plot": 3.5},
		},
	}
	s := ScoreAxisMatch(b, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score below threshold, got %+v", s)
	}
}

func TestScoreAxisMatch_GracefulWhenNoDimensionedRatings(t *testing.T) {
	b := store.BookRow{Categories: []string{"sci-fi"}}
	p := &profile.Profile{} // ShelfAxisMeans nil — fresh post-migration user
	s := ScoreAxisMatch(b, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score for empty ShelfAxisMeans, got %+v", s)
	}
}

func TestScoreAxisMatch_PicksHighestAcrossShelves(t *testing.T) {
	b := store.BookRow{Categories: []string{"sci-fi", "fantasy"}}
	p := &profile.Profile{
		ShelfAxisMeans: map[string]map[string]float64{
			"sci-fi":  {"plot": 4.2},
			"fantasy": {"characters": 4.9}, // highest
		},
	}
	s := ScoreAxisMatch(b, p)
	if !strings.Contains(s.Reason, "Characters") || !strings.Contains(s.Reason, "fantasy") {
		t.Errorf("Reason = %q, want highest-mean (Characters, fantasy) chosen", s.Reason)
	}
}

func TestScoreAxisMatch_TiebreakDeterministic(t *testing.T) {
	// Two identical means — alpha-by-axis then alpha-by-shelf wins.
	// Axis "characters" < "plot"; "fantasy" < "sci-fi".
	b := store.BookRow{Categories: []string{"sci-fi", "fantasy"}}
	p := &profile.Profile{
		ShelfAxisMeans: map[string]map[string]float64{
			"sci-fi":  {"plot": 4.5},
			"fantasy": {"characters": 4.5},
		},
	}
	s := ScoreAxisMatch(b, p)
	if !strings.Contains(s.Reason, "Characters") || !strings.Contains(s.Reason, "fantasy") {
		t.Errorf("Reason = %q, want characters/fantasy by alpha tiebreak", s.Reason)
	}
}

func TestScoreAxisMatch_UnknownAxisLabelFallsBackToKey(t *testing.T) {
	b := store.BookRow{Categories: []string{"sci-fi"}}
	p := &profile.Profile{
		ShelfAxisMeans: map[string]map[string]float64{
			"sci-fi": {"unknown_axis": 4.5},
		},
	}
	s := ScoreAxisMatch(b, p)
	if !strings.Contains(s.Reason, "unknown_axis") {
		t.Errorf("Reason = %q, want fallback to raw axis key", s.Reason)
	}
}
