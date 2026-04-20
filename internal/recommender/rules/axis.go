package rules

import (
	"fmt"
	"sort"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// axisHighThreshold is the per-(shelf, axis) mean above which the user
// is considered to "rate this axis highly on this shelf". 4.0 on the
// 0–5 scale matches the SKILL.md example ("You rate Plot highly on
// sci-fi shelves, mean 4.8"). v0.5 LLM tunes this from rated review
// text; for v0.3 it's a reasonable hand-picked cutoff.
const axisHighThreshold = 4.0

// axisValueFloor is the rating value subtracted from the matched mean
// before the (mean - floor) / 2 normalization that maps the >= 4.0
// threshold band into the [0.5, 1.0] Value range. Picked so a 4.0 mean
// barely qualifies (Value 0.5) and a perfect 5.0 saturates (Value 1.0).
const axisValueFloor = 3.0

// ScoreAxisMatch finds the highest-mean (shelf, axis) entry in
// p.ShelfAxisMeans that the candidate's Categories overlap and that
// clears axisHighThreshold. Returns Score{} when:
//   - p.ShelfAxisMeans is empty (graceful degradation: fresh post-
//     migration user with no dimensioned ratings yet), or
//   - no candidate-shelf, qualifying-axis pair exists.
//
// Tiebreak when two pairs have identical means: alphabetical by axis
// then by shelf — keeps the surfaced reason stable across runs.
func ScoreAxisMatch(b store.BookRow, p *profile.Profile) Score {
	if len(p.ShelfAxisMeans) == 0 || len(b.Categories) == 0 {
		return Score{}
	}
	type cand struct {
		shelf, axis string
		mean        float64
	}
	var best *cand
	shelves := append([]string(nil), b.Categories...)
	sort.Strings(shelves)
	for _, shelf := range shelves {
		am, ok := p.ShelfAxisMeans[shelf]
		if !ok {
			continue
		}
		axes := make([]string, 0, len(am))
		for k := range am {
			axes = append(axes, k)
		}
		sort.Strings(axes)
		for _, axis := range axes {
			m := am[axis]
			if m < axisHighThreshold {
				continue
			}
			if best == nil || m > best.mean {
				best = &cand{shelf: shelf, axis: axis, mean: m}
			}
		}
	}
	if best == nil {
		return Score{}
	}
	v := (best.mean - axisValueFloor) / 2.0
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	label, ok := frontmatter.RatingAxisLabels[best.axis]
	if !ok || label == "" {
		label = best.axis
	}
	return Score{
		Value:  v,
		Reason: fmt.Sprintf("You rate %s highly on %s (mean %.1f)", label, best.shelf, best.mean),
	}
}
