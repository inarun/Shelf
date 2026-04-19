package body

import (
	"bytes"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

func TestParseRatingSection(t *testing.T) {
	input := []byte(`# A

## Rating — ★ 4/5

*Trial System*
Emotional Impact: 4
Characters: 5
Plot: 3

## Notes

x
`)
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	r := b.Rating()
	if r == nil {
		t.Fatalf("Rating() = nil")
	}
	if r.Values["emotional_impact"] != 4 {
		t.Errorf("emotional_impact got %d", r.Values["emotional_impact"])
	}
	if r.Values["characters"] != 5 {
		t.Errorf("characters got %d", r.Values["characters"])
	}
	if r.Values["plot"] != 3 {
		t.Errorf("plot got %d", r.Values["plot"])
	}
}

func TestParseRatingHeadingVariants(t *testing.T) {
	cases := []string{
		"## Rating\n\nEmotional Impact: 5\n",
		"## Rating — ★ 6/5\n\nEmotional Impact: 5\n",
		"## Rating - 4/5\n\nEmotional Impact: 4\n",
		"## Rating \u2013 ★ 3/5\n\nEmotional Impact: 3\n",
	}
	for i, c := range cases {
		b, err := Parse([]byte("# T\n\n" + c))
		if err != nil {
			t.Fatalf("case %d parse: %v", i, err)
		}
		if b.Rating() == nil {
			t.Errorf("case %d: Rating() nil; input=%q", i, c)
		}
	}
}

func TestRatingHeadingDoesNotMatchPlural(t *testing.T) {
	input := []byte("# T\n\n## Ratings\n\nfoo\n")
	b, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if b.Rating() != nil {
		t.Errorf("`## Ratings` should not classify as Rating")
	}
}

func TestSetRatingFromFrontmatterCreatesSection(t *testing.T) {
	b, _ := Parse([]byte("# T\n\n## Notes\n\nx\n"))
	over := 6.0
	b.SetRatingFromFrontmatter(&frontmatter.Rating{
		TrialSystem: map[string]int{"emotional_impact": 5, "characters": 5, "plot": 5, "dialogue_prose": 5, "cinematography_worldbuilding": 5},
		Overall:     &over,
	})
	out := string(b.Serialize())
	if !strings.Contains(out, "## Rating — ★ 6/5") {
		t.Errorf("heading missing; output:\n%s", out)
	}
	if !strings.Contains(out, "Emotional Impact: 5") {
		t.Errorf("axis line missing; output:\n%s", out)
	}
	if !strings.Contains(out, "*Trial System*") {
		t.Errorf("Trial System marker missing; output:\n%s", out)
	}
}

func TestRatingSectionAtCanonicalPosition(t *testing.T) {
	b, _ := Parse([]byte("# T\n\n## Notes\n\nx\n"))
	b.SetRatingFromFrontmatter(&frontmatter.Rating{
		TrialSystem: map[string]int{"plot": 4},
	})
	// The Rating block should land between H1 and Notes (pos 1).
	if len(b.Blocks) < 3 {
		t.Fatalf("expected >= 3 blocks, got %d", len(b.Blocks))
	}
	if b.Blocks[0].Kind != KindH1 || b.Blocks[1].Kind != KindRating || b.Blocks[2].Kind != KindNotes {
		t.Errorf("kind order got %v %v %v, want H1 Rating Notes",
			b.Blocks[0].Kind, b.Blocks[1].Kind, b.Blocks[2].Kind)
	}
}

func TestSetRatingFromFrontmatterNilRemoves(t *testing.T) {
	b, _ := Parse([]byte(`# T

## Rating — ★ 4/5

*Trial System*
Plot: 4

## Notes

x
`))
	if b.Rating() == nil {
		t.Fatalf("setup: Rating() nil after parse")
	}
	b.SetRatingFromFrontmatter(nil)
	if b.Rating() != nil {
		t.Errorf("Rating block should have been removed; got %v", b.Rating())
	}
	if bytes.Contains(b.Serialize(), []byte("## Rating")) {
		t.Errorf("serialized output still contains `## Rating`:\n%s", b.Serialize())
	}
}

func TestSetRatingFromFrontmatterEmptyRemoves(t *testing.T) {
	b, _ := Parse([]byte("# T\n\n## Rating\n\nPlot: 4\n\n## Notes\n\nx\n"))
	b.SetRatingFromFrontmatter(&frontmatter.Rating{})
	if b.Rating() != nil {
		t.Errorf("empty Rating should remove the block")
	}
}

func TestRatingOverrideHeading(t *testing.T) {
	b, _ := Parse([]byte("# T\n"))
	over := 6.0
	b.SetRatingFromFrontmatter(&frontmatter.Rating{
		TrialSystem: map[string]int{"plot": 3}, // mean would be 3 without override
		Overall:     &over,
	})
	out := string(b.Serialize())
	// Heading should reflect the override, not the mean.
	if !strings.Contains(out, "## Rating — ★ 6/5") {
		t.Errorf("heading should use override; output:\n%s", out)
	}
}

func TestAsFrontmatterRating(t *testing.T) {
	p := &RatingParsed{Values: map[string]int{"plot": 5}}
	r := p.AsFrontmatterRating()
	if r == nil || !r.IsDimensioned() || r.TrialSystem["plot"] != 5 {
		t.Errorf("AsFrontmatterRating got %v", r)
	}
	if r.HasOverride() {
		t.Errorf("body parse should not carry override")
	}
}

func TestH1BlockNoLongerEmitsRatingLine(t *testing.T) {
	// Parsing preserves a legacy "Rating — N/5" line in H1 Raw bytes
	// (untouched → round-trips). But when H1 is re-generated (dirty)
	// post-S15, the rating line must NOT be emitted — rating now lives
	// in the `## Rating` section.
	b, _ := Parse([]byte("# Old\n\nRating — 4/5\n\n## Notes\n\nx\n"))
	b.SetTitle("New")
	out := string(b.Serialize())
	if strings.Contains(out, "Rating — 4/5") {
		t.Errorf("dirty H1 should not re-emit legacy rating line; output:\n%s", out)
	}
}
