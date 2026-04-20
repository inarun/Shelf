package rules

import (
	"sort"

	"github.com/inarun/Shelf/internal/domain/series"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

// Score is the output of a single scorer. Value is normalized to [0, 1];
// the combined ranker multiplies it by the per-scorer Weight before
// summing. Reason is the user-facing string surfaced in the top-three
// list — empty Reason means "no contribution to surface" (Value may
// still be non-zero, e.g. LengthMatch below its surfacing threshold).
type Score struct {
	Value  float64
	Reason string
}

// Weights vectorizes the per-scorer multipliers. Hardcoded as
// DefaultWeights for v0.3; v0.5's LLM-enhanced recommender tunes these
// (and the AxisMatch threshold constants in axis.go) from rated review
// text. Scale is "1.0 = baseline shelf signal" — values above 1.0 mean
// the scorer carries a stronger prior than a simple genre match, values
// below mean a weaker / softer signal.
type Weights struct {
	SeriesCompletion float64
	AuthorAffinity   float64
	ShelfSimilarity  float64
	LengthMatch      float64
	GenreMatch       float64
	AxisMatch        float64
}

// DefaultWeights — seed values chosen by hand for Session 18:
//   - SeriesCompletion 1.5 — the strongest single signal (a reading
//     journal exists in part to help the user finish what they started).
//   - AuthorAffinity 1.2 — past-author preference is a strong but not
//     overwhelming signal; some users diversify deliberately.
//   - ShelfSimilarity 1.0 — Jaccard over Categories ↔ TopShelves is the
//     baseline genre match.
//   - AxisMatch 1.0 — comparable strength to Jaccard but orthogonal axis.
//   - GenreMatch 0.6 — subordinate to Jaccard; this is the soft "did
//     this hit any top genre" coverage signal kept per the spec.
//   - LengthMatch 0.4 — softest preference, not strongly predictive.
var DefaultWeights = Weights{
	SeriesCompletion: 1.5,
	AuthorAffinity:   1.2,
	ShelfSimilarity:  1.0,
	LengthMatch:      0.4,
	GenreMatch:       0.6,
	AxisMatch:        1.0,
}

// ScoredBook is the JSON shape of one entry in the /api/recommendations
// response. Mirrors the subset of store.BookRow the SSR card needs to
// render — deliberately omits ID, MtimeNanos, IndexedAtUnix, and other
// implementation detail. Reasons is always a non-nil slice so the
// frontend can iterate without a null check.
type ScoredBook struct {
	Filename    string   `json:"filename"`
	Title       string   `json:"title"`
	Subtitle    string   `json:"subtitle,omitempty"`
	Authors     []string `json:"authors"`
	Series      string   `json:"series,omitempty"`
	SeriesIndex *float64 `json:"series_index,omitempty"`
	Categories  []string `json:"categories"`
	Score       float64  `json:"score"`
	Reasons     []string `json:"reasons"`
}

// maxReasons caps the user-facing reasons per book. Three is enough to
// explain a recommendation without crowding the SSR card.
const maxReasons = 3

// Rank scores every candidate using the six scorers, applies w, and
// returns descending by combined score. Each ScoredBook carries the top
// maxReasons non-empty reasons sorted by weighted contribution.
//
// seriesStates is computed once by the caller (handler) over the full
// library so SeriesCompletion can find gaps the candidate fills. Pure;
// safe to call with empty inputs (returns an empty slice).
func Rank(candidates []store.BookRow, p *profile.Profile, seriesStates []series.State, w Weights) []ScoredBook {
	out := make([]ScoredBook, 0, len(candidates))
	for _, b := range candidates {
		out = append(out, scoreOne(b, p, seriesStates, w))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Filename < out[j].Filename
	})
	return out
}

// scoreOne computes the per-book combined score and Reasons. Pulled out
// of Rank so a future caller (e.g., a single-book "why?" debug endpoint)
// can reuse it without paying the sort cost.
func scoreOne(b store.BookRow, p *profile.Profile, ss []series.State, w Weights) ScoredBook {
	type contrib struct {
		value  float64
		reason string
	}
	raw := []struct {
		s Score
		w float64
	}{
		{ScoreSeriesCompletion(b, p, ss), w.SeriesCompletion},
		{ScoreAuthorAffinity(b, p), w.AuthorAffinity},
		{ScoreShelfSimilarity(b, p), w.ShelfSimilarity},
		{ScoreLengthMatch(b, p), w.LengthMatch},
		{ScoreGenreMatch(b, p), w.GenreMatch},
		{ScoreAxisMatch(b, p), w.AxisMatch},
	}

	var combined float64
	withReason := make([]contrib, 0, len(raw))
	for _, r := range raw {
		c := r.s.Value * r.w
		combined += c
		if r.s.Reason != "" {
			withReason = append(withReason, contrib{value: c, reason: r.s.Reason})
		}
	}
	sort.SliceStable(withReason, func(i, j int) bool {
		return withReason[i].value > withReason[j].value
	})
	reasons := make([]string, 0, maxReasons)
	for i := 0; i < len(withReason) && i < maxReasons; i++ {
		reasons = append(reasons, withReason[i].reason)
	}

	return ScoredBook{
		Filename:    b.Filename,
		Title:       b.Title,
		Subtitle:    b.Subtitle,
		Authors:     b.Authors,
		Series:      b.SeriesName,
		SeriesIndex: b.SeriesIndex,
		Categories:  b.Categories,
		Score:       combined,
		Reasons:     reasons,
	}
}
