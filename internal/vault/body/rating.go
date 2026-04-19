package body

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// Rating returns the parsed `## Rating` block or nil when absent.
func (b *Body) Rating() *RatingParsed {
	idx := b.indexOf(KindRating)
	if idx < 0 {
		return nil
	}
	p, ok := b.Blocks[idx].Parsed.(*RatingParsed)
	if !ok || p == nil {
		return nil
	}
	return p
}

// AsFrontmatterRating wraps the parsed body rating as a
// *frontmatter.Rating for reader-fallback flows. Only TrialSystem axis
// values round-trip through the body; Overall is always nil here since
// body lines don't carry an explicit override — callers needing the
// override consult frontmatter directly.
func (p *RatingParsed) AsFrontmatterRating() *frontmatter.Rating {
	if p == nil || len(p.Values) == 0 {
		return nil
	}
	axes := make(map[string]int, len(p.Values))
	for k, v := range p.Values {
		axes[k] = v
	}
	return &frontmatter.Rating{TrialSystem: axes}
}

// SetRatingFromFrontmatter regenerates the `## Rating` block from the
// supplied frontmatter rating. When r is nil or empty the block is
// removed entirely. When present, the block is created (if absent) at
// its canonical position between KindH1 and KindKeyIdeas, and its
// contents are rewritten — any user edits to the existing block are
// overwritten per the dual-write rule (SKILL.md §v0.2.1).
func (b *Body) SetRatingFromFrontmatter(r *frontmatter.Rating) {
	if r == nil || r.IsEmpty() {
		b.removeSection(KindRating)
		return
	}
	b.EnsureSection(KindRating)
	idx := b.indexOf(KindRating)
	bl := &b.Blocks[idx]
	if bl.Parsed == nil {
		bl.Parsed = &RatingParsed{}
	}
	p := bl.Parsed.(*RatingParsed)
	p.Values = map[string]int{}
	for k, v := range r.TrialSystem {
		p.Values[k] = v
	}
	if r.Overall != nil {
		v := *r.Overall
		p.OverrideOverall = &v
	} else {
		p.OverrideOverall = nil
	}
	bl.dirty = true
}

// removeSection deletes the first block of the given kind (if present).
// No-op otherwise.
func (b *Body) removeSection(kind Kind) {
	idx := b.indexOf(kind)
	if idx < 0 {
		return
	}
	b.Blocks = append(b.Blocks[:idx], b.Blocks[idx+1:]...)
}

// parseRatingSection extracts axis values from body lines shaped
// "Label: int". The leading "*Trial System*" marker line (or similar
// italic annotation) is skipped. Labels are matched case- and
// whitespace-insensitively against RatingAxisLabels so a user writing
// "Dialogue / Prose" instead of "Dialogue/Prose" still round-trips.
func parseRatingSection(region []byte) *RatingParsed {
	content := bodyAfterHeading(region)
	out := &RatingParsed{Values: map[string]int{}}
	for _, line := range splitLinesKeepEOL(content) {
		trimmed := strings.TrimRight(string(line), "\r\n")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		// Skip the italicised Trial System marker line.
		if strings.HasPrefix(trimmed, "*") && strings.HasSuffix(trimmed, "*") {
			continue
		}
		colon := strings.Index(trimmed, ":")
		if colon < 0 {
			continue
		}
		label := strings.TrimSpace(trimmed[:colon])
		valueStr := strings.TrimSpace(trimmed[colon+1:])
		n, err := strconv.Atoi(valueStr)
		if err != nil {
			continue
		}
		key := labelToAxisKey(label)
		if key == "" {
			continue
		}
		out.Values[key] = n
	}
	if len(out.Values) == 0 {
		return out
	}
	return out
}

// labelToAxisKey maps a human-facing axis label to its snake_case key,
// or "" when no match. Whitespace-insensitive: "Dialogue / Prose",
// "Dialogue/Prose", and " dialogue/prose " all map to "dialogue_prose".
func labelToAxisKey(label string) string {
	norm := strings.ToLower(label)
	norm = whitespaceRe.ReplaceAllString(norm, "")
	for key, canonical := range frontmatter.RatingAxisLabels {
		c := strings.ToLower(canonical)
		c = whitespaceRe.ReplaceAllString(c, "")
		if c == norm {
			return key
		}
	}
	return ""
}

var whitespaceRe = regexp.MustCompile(`\s+`)

// regenerateRating produces the canonical body form of a Rating block.
// Heading is "## Rating — ★ {formatOverall}/5"; the body is the
// "*Trial System*" marker line followed by one "Label: int" per
// present axis, in canonical RatingAxes order. overall is the
// pre-computed effective value (-1 when unknown; caller guards).
func regenerateRating(p *RatingParsed, overall float64) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "## Rating — ★ %s/5\n\n", formatOverall(overall))
	if p == nil || len(p.Values) == 0 {
		// Overall-only rating: no axis lines to emit.
		buf.WriteString("\n")
		return buf.Bytes()
	}
	buf.WriteString("*Trial System*\n")
	for _, axis := range frontmatter.RatingAxes {
		v, ok := p.Values[axis]
		if !ok {
			continue
		}
		fmt.Fprintf(&buf, "%s: %d\n", frontmatter.RatingAxisLabels[axis], v)
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

// formatOverall renders the effective overall score with at most one
// decimal place, trimming trailing zeros — "5" for integers, "4.5" for
// halves, "4.6" for means.
func formatOverall(v float64) string {
	rounded := math.Round(v*10) / 10
	if rounded == float64(int64(rounded)) {
		return strconv.FormatInt(int64(rounded), 10)
	}
	return strconv.FormatFloat(rounded, 'f', 1, 64)
}
