package rules

import (
	"math"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

func pi(v int64) *int64 { return &v }

func TestScoreLengthMatch_AtMeanReturnsOne(t *testing.T) {
	stdev := 100.0
	b := store.BookRow{TotalPages: pi(400)}
	p := &profile.Profile{LengthMean: 400, LengthStdev: &stdev}
	s := ScoreLengthMatch(b, p)
	if s.Value != 1.0 {
		t.Errorf("Value = %v, want 1.0 at mean", s.Value)
	}
	if !strings.Contains(s.Reason, "400p") {
		t.Errorf("Reason = %q, want it to mention ~400p", s.Reason)
	}
}

func TestScoreLengthMatch_OneStdevAway(t *testing.T) {
	stdev := 100.0
	b := store.BookRow{TotalPages: pi(500)}
	p := &profile.Profile{LengthMean: 400, LengthStdev: &stdev}
	s := ScoreLengthMatch(b, p)
	want := math.Exp(-0.5)
	if math.Abs(s.Value-want) > 1e-9 {
		t.Errorf("Value = %v, want %v", s.Value, want)
	}
	if s.Reason == "" {
		t.Errorf("Reason should be present (Value %v > threshold)", s.Value)
	}
}

func TestScoreLengthMatch_TwoStdevAway(t *testing.T) {
	stdev := 100.0
	b := store.BookRow{TotalPages: pi(600)}
	p := &profile.Profile{LengthMean: 400, LengthStdev: &stdev}
	s := ScoreLengthMatch(b, p)
	want := math.Exp(-2)
	if math.Abs(s.Value-want) > 1e-9 {
		t.Errorf("Value = %v, want %v", s.Value, want)
	}
	if s.Reason != "" {
		t.Errorf("Reason should be suppressed below threshold, got %q", s.Reason)
	}
}

func TestScoreLengthMatch_NilTotalPages(t *testing.T) {
	stdev := 100.0
	p := &profile.Profile{LengthMean: 400, LengthStdev: &stdev}
	s := ScoreLengthMatch(store.BookRow{}, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score for nil pages, got %+v", s)
	}
}

func TestScoreLengthMatch_NilLengthStdevDegrades(t *testing.T) {
	// Single-sample / fresh user: LengthStdev is nil → graceful zero.
	b := store.BookRow{TotalPages: pi(400)}
	p := &profile.Profile{LengthMean: 400}
	s := ScoreLengthMatch(b, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score for nil stdev, got %+v", s)
	}
}
