package rules

import (
	"math"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

func TestScoreShelfSimilarity_JaccardCorrectness(t *testing.T) {
	// |A| = {sci-fi, space-opera, classics}, |B| = {sci-fi, fantasy}.
	// Intersection = {sci-fi}, union = 4. Expected v = 1/4 = 0.25.
	b := store.BookRow{Categories: []string{"sci-fi", "space-opera", "classics"}}
	p := &profile.Profile{TopShelves: []string{"sci-fi", "fantasy"}}
	s := ScoreShelfSimilarity(b, p)
	if math.Abs(s.Value-0.25) > 1e-9 {
		t.Errorf("Value = %v, want 0.25", s.Value)
	}
	if !strings.Contains(s.Reason, "sci-fi") {
		t.Errorf("Reason = %q, want it to mention sci-fi", s.Reason)
	}
}

func TestScoreShelfSimilarity_NoOverlapZero(t *testing.T) {
	b := store.BookRow{Categories: []string{"history"}}
	p := &profile.Profile{TopShelves: []string{"sci-fi"}}
	s := ScoreShelfSimilarity(b, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score, got %+v", s)
	}
}

func TestScoreShelfSimilarity_EmptyTopShelvesGraceful(t *testing.T) {
	b := store.BookRow{Categories: []string{"sci-fi"}}
	p := &profile.Profile{}
	s := ScoreShelfSimilarity(b, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score for fresh user, got %+v", s)
	}
}

func TestScoreGenreMatch_SingleHitSingularReason(t *testing.T) {
	b := store.BookRow{Categories: []string{"sci-fi", "novella"}}
	p := &profile.Profile{TopShelves: []string{"sci-fi", "fantasy", "horror"}}
	s := ScoreGenreMatch(b, p)
	// |∩| = 1, |TopShelves| = 3 → v = 1/3.
	if math.Abs(s.Value-1.0/3.0) > 1e-9 {
		t.Errorf("Value = %v, want 1/3", s.Value)
	}
	if !strings.HasPrefix(s.Reason, "On your top shelf:") {
		t.Errorf("Reason = %q, want singular form", s.Reason)
	}
}

func TestScoreGenreMatch_MultiHitPluralReason(t *testing.T) {
	b := store.BookRow{Categories: []string{"sci-fi", "fantasy"}}
	p := &profile.Profile{TopShelves: []string{"sci-fi", "fantasy", "horror"}}
	s := ScoreGenreMatch(b, p)
	if math.Abs(s.Value-2.0/3.0) > 1e-9 {
		t.Errorf("Value = %v, want 2/3", s.Value)
	}
	if !strings.HasPrefix(s.Reason, "On your top shelves:") {
		t.Errorf("Reason = %q, want plural form", s.Reason)
	}
}

func TestScoreGenreMatch_NoOverlapZero(t *testing.T) {
	b := store.BookRow{Categories: []string{"history"}}
	p := &profile.Profile{TopShelves: []string{"sci-fi"}}
	s := ScoreGenreMatch(b, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score, got %+v", s)
	}
}

func TestScoreGenreMatch_ClampsToOne(t *testing.T) {
	// |∩| can equal |TopShelves| when every top shelf is matched. v = 1.0.
	b := store.BookRow{Categories: []string{"sci-fi", "extras"}}
	p := &profile.Profile{TopShelves: []string{"sci-fi"}}
	s := ScoreGenreMatch(b, p)
	if s.Value != 1.0 {
		t.Errorf("Value = %v, want 1.0 when intersection covers all top shelves", s.Value)
	}
}
