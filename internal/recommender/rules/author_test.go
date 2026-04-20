package rules

import (
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

func TestScoreAuthorAffinity_HitFirstAuthor(t *testing.T) {
	b := store.BookRow{Authors: []string{"Le Guin"}}
	p := &profile.Profile{TopAuthors: []string{"Le Guin", "Simmons"}}
	s := ScoreAuthorAffinity(b, p)
	if s.Value != 1.0 {
		t.Errorf("Value = %v, want 1.0", s.Value)
	}
	if !strings.Contains(s.Reason, "Le Guin") {
		t.Errorf("Reason = %q, want it to mention Le Guin", s.Reason)
	}
}

func TestScoreAuthorAffinity_HitSecondAuthor(t *testing.T) {
	b := store.BookRow{Authors: []string{"Pratchett", "Gaiman"}}
	p := &profile.Profile{TopAuthors: []string{"Gaiman"}}
	s := ScoreAuthorAffinity(b, p)
	if s.Value != 1.0 {
		t.Errorf("Value = %v, want 1.0", s.Value)
	}
	if !strings.Contains(s.Reason, "Gaiman") {
		t.Errorf("Reason = %q, want it to mention Gaiman", s.Reason)
	}
}

func TestScoreAuthorAffinity_NoOverlap(t *testing.T) {
	b := store.BookRow{Authors: []string{"Unknown"}}
	p := &profile.Profile{TopAuthors: []string{"Le Guin"}}
	s := ScoreAuthorAffinity(b, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score, got %+v", s)
	}
}

func TestScoreAuthorAffinity_EmptyTopAuthorsGraceful(t *testing.T) {
	b := store.BookRow{Authors: []string{"Le Guin"}}
	p := &profile.Profile{}
	s := ScoreAuthorAffinity(b, p)
	if s.Value != 0 || s.Reason != "" {
		t.Errorf("expected zero Score for fresh user, got %+v", s)
	}
}
