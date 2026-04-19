package frontmatter

import (
	"math"
	"strings"
	"testing"
)

func TestParseRatingLegacyScalar(t *testing.T) {
	doc := []byte("---\nrating: 4\n---\nbody\n")
	f, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := f.Rating()
	if r == nil {
		t.Fatalf("Rating() = nil, want non-nil")
	}
	if r.IsDimensioned() {
		t.Errorf("IsDimensioned() = true, want false for legacy scalar")
	}
	if !r.HasOverride() {
		t.Errorf("HasOverride() = false, want true")
	}
	if *r.Overall != 4 {
		t.Errorf("Overall = %v, want 4", *r.Overall)
	}
}

func TestParseRatingMapShape(t *testing.T) {
	doc := []byte(`---
rating:
  trial_system:
    emotional_impact: 5
    characters: 4
    plot: 5
    dialogue_prose: 3
    cinematography_worldbuilding: 5
  overall: 6
---
body
`)
	f, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := f.Rating()
	if r == nil {
		t.Fatalf("Rating() = nil")
	}
	if !r.IsDimensioned() {
		t.Errorf("IsDimensioned() = false, want true")
	}
	if r.TrialSystem["emotional_impact"] != 5 {
		t.Errorf("emotional_impact = %d, want 5", r.TrialSystem["emotional_impact"])
	}
	if r.TrialSystem["dialogue_prose"] != 3 {
		t.Errorf("dialogue_prose = %d, want 3", r.TrialSystem["dialogue_prose"])
	}
	if !r.HasOverride() || *r.Overall != 6 {
		t.Errorf("Overall override missing/wrong: %v", r.Overall)
	}
}

func TestParseRatingMapShapeNoOverride(t *testing.T) {
	doc := []byte(`---
rating:
  trial_system:
    plot: 4
    characters: 5
---
body
`)
	f, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := f.Rating()
	if r == nil || r.HasOverride() {
		t.Fatalf("Rating=%v HasOverride=%v", r, r.HasOverride())
	}
	if len(r.TrialSystem) != 2 {
		t.Errorf("TrialSystem len = %d, want 2", len(r.TrialSystem))
	}
}

func TestParseRatingAbsent(t *testing.T) {
	doc := []byte("---\ntitle: foo\n---\nbody\n")
	f, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r := f.Rating(); r != nil {
		t.Errorf("Rating() = %v, want nil", r)
	}
}

func TestParseRatingNull(t *testing.T) {
	doc := []byte("---\nrating: null\n---\nbody\n")
	f, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if r := f.Rating(); r != nil {
		t.Errorf("Rating() = %v, want nil for null scalar", r)
	}
}

func TestParseRatingIgnoresUnknownAxis(t *testing.T) {
	doc := []byte(`---
rating:
  trial_system:
    emotional_impact: 5
    rogue_axis: 9
---
body
`)
	f, _, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := f.Rating()
	if r == nil {
		t.Fatalf("Rating() = nil")
	}
	if _, ok := r.TrialSystem["rogue_axis"]; ok {
		t.Errorf("rogue_axis should have been filtered out")
	}
	if r.TrialSystem["emotional_impact"] != 5 {
		t.Errorf("emotional_impact not preserved")
	}
}

func TestSerializeRatingEmitsMapShape(t *testing.T) {
	f := NewEmpty()
	override := 6.0
	err := f.SetRating(&Rating{
		TrialSystem: map[string]int{
			"emotional_impact":             5,
			"characters":                   4,
			"plot":                         5,
			"dialogue_prose":               3,
			"cinematography_worldbuilding": 5,
		},
		Overall: &override,
	})
	if err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	out, err := f.Serialize(nil)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "trial_system:") {
		t.Errorf("no trial_system: key in output:\n%s", s)
	}
	if !strings.Contains(s, "emotional_impact: 5") {
		t.Errorf("missing emotional_impact: 5")
	}
	if !strings.Contains(s, "overall: 6") {
		t.Errorf("missing overall: 6")
	}
	// Canonical axis order.
	idxEmo := strings.Index(s, "emotional_impact:")
	idxChar := strings.Index(s, "characters:")
	idxPlot := strings.Index(s, "plot:")
	if !(idxEmo < idxChar && idxChar < idxPlot) {
		t.Errorf("axis order not canonical: emo=%d char=%d plot=%d\n%s",
			idxEmo, idxChar, idxPlot, s)
	}
}

func TestSerializeRatingOverrideOnly(t *testing.T) {
	f := NewEmpty()
	v := 4.0
	if err := f.SetRating(&Rating{Overall: &v}); err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	out, _ := f.Serialize(nil)
	s := string(out)
	if strings.Contains(s, "trial_system:") {
		t.Errorf("trial_system: should be absent when axes empty:\n%s", s)
	}
	if !strings.Contains(s, "overall: 4") {
		t.Errorf("missing overall: 4")
	}
}

func TestSetRatingNilClears(t *testing.T) {
	f := NewEmpty()
	v := 5.0
	_ = f.SetRating(&Rating{Overall: &v})
	if r := f.Rating(); r == nil {
		t.Fatalf("setup: Rating() nil after SetRating")
	}
	if err := f.SetRating(nil); err != nil {
		t.Fatalf("SetRating(nil): %v", err)
	}
	if r := f.Rating(); r != nil {
		t.Errorf("Rating() = %v, want nil after SetRating(nil)", r)
	}
}

func TestSetRatingEmptyClears(t *testing.T) {
	f := NewEmpty()
	if err := f.SetRating(&Rating{}); err != nil {
		t.Fatalf("SetRating(empty): %v", err)
	}
	if r := f.Rating(); r != nil {
		t.Errorf("Rating() = %v, want nil for empty Rating", r)
	}
}

func TestSetRatingRejectsUnknownAxis(t *testing.T) {
	f := NewEmpty()
	err := f.SetRating(&Rating{TrialSystem: map[string]int{"rogue": 3}})
	if err == nil {
		t.Fatalf("SetRating with unknown axis: want error, got nil")
	}
}

func TestSetRatingRejectsNegativeAxis(t *testing.T) {
	f := NewEmpty()
	err := f.SetRating(&Rating{TrialSystem: map[string]int{"plot": -1}})
	if err == nil {
		t.Fatalf("SetRating with negative axis: want error, got nil")
	}
}

func TestSetRatingRejectsOverallOutOfRange(t *testing.T) {
	f := NewEmpty()
	over := 11.0
	err := f.SetRating(&Rating{Overall: &over})
	if err == nil {
		t.Fatalf("SetRating with overall=11: want error, got nil")
	}
	neg := -1.0
	err = f.SetRating(&Rating{Overall: &neg})
	if err == nil {
		t.Fatalf("SetRating with overall=-1: want error, got nil")
	}
}

func TestEffectiveWithOverride(t *testing.T) {
	v := 6.5
	r := &Rating{
		TrialSystem: map[string]int{"plot": 3},
		Overall:     &v,
	}
	if got := r.Effective(); got != 6.5 {
		t.Errorf("Effective() = %v, want 6.5 (override wins)", got)
	}
}

func TestEffectiveWithoutOverride(t *testing.T) {
	r := &Rating{
		TrialSystem: map[string]int{
			"plot":             4,
			"characters":       4,
			"emotional_impact": 4,
		},
	}
	want := 4.0
	if got := r.Effective(); math.Abs(got-want) > 1e-9 {
		t.Errorf("Effective() = %v, want %v", got, want)
	}
}

func TestEffectiveEmpty(t *testing.T) {
	r := &Rating{}
	if got := r.Effective(); got != 0 {
		t.Errorf("Effective() empty = %v, want 0", got)
	}
}

func TestEffectiveRoundedNilOnEmpty(t *testing.T) {
	if (*Rating)(nil).EffectiveRounded() != nil {
		t.Errorf("EffectiveRounded on nil Rating: want nil")
	}
	if (&Rating{}).EffectiveRounded() != nil {
		t.Errorf("EffectiveRounded on empty: want nil")
	}
}

func TestEffectiveRoundedMatches(t *testing.T) {
	r := &Rating{TrialSystem: map[string]int{
		"plot": 4, "characters": 5, "emotional_impact": 5,
	}}
	got := r.EffectiveRounded()
	if got == nil || *got != 5 {
		t.Errorf("EffectiveRounded = %v, want 5 (round 4.666…)", got)
	}
}

func TestSetRatingMapRoundTrips(t *testing.T) {
	f := NewEmpty()
	over := 6.0
	err := f.SetRating(&Rating{
		TrialSystem: map[string]int{
			"emotional_impact": 5, "characters": 5, "plot": 5,
			"dialogue_prose": 5, "cinematography_worldbuilding": 5,
		},
		Overall: &over,
	})
	if err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	out, _ := f.Serialize(nil)
	// Re-parse.
	f2, _, err := Parse(out)
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}
	r2 := f2.Rating()
	if r2 == nil {
		t.Fatalf("re-parse Rating() = nil")
	}
	if len(r2.TrialSystem) != 5 {
		t.Errorf("trial_system len = %d, want 5", len(r2.TrialSystem))
	}
	if !r2.HasOverride() || *r2.Overall != 6 {
		t.Errorf("overall not preserved: %v", r2.Overall)
	}
}
