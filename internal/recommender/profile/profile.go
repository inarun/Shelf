package profile

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/inarun/Shelf/internal/domain/series"
	"github.com/inarun/Shelf/internal/index/store"
)

// Profile is the taste summary the Session 18 scorers consume. Every
// numeric field is derived by pure computation over the store snapshot
// Extract captures — no filesystem or network access, no state kept
// between calls.
//
// Recency weighting uses an exponential half-life of 365 days: a book
// finished today carries weight 1.0, a book finished a year ago 0.5, two
// years ago 0.25, and so on. A book with no finish date falls back to
// the latest start date; neither-present books carry weight 1.0 (we
// treat them as "rated without a date" — v0.1 notes from before dates
// were tracked).
//
// Stdev fields are suppressed (axis key dropped, LengthStdev == nil)
// when fewer than two samples contribute. Stdev uses the population
// formula (Σw · (v − μ)² / Σw); a sample-stdev correction with
// reliability weights has no clean denominator and adds nothing at
// recommender granularity.
type Profile struct {
	TopAuthors       []string           `json:"top_authors"`
	TopShelves       []string           `json:"top_shelves"`
	AxisMeans        map[string]float64 `json:"axis_means"`
	AxisStdevs       map[string]float64 `json:"axis_stdevs"`
	LengthMean       float64            `json:"length_mean"`
	LengthStdev      *float64           `json:"length_stdev"`
	SeriesInProgress []string           `json:"series_in_progress"`
	RatingMean       float64            `json:"rating_mean"`
	BookCount        int                `json:"book_count"`
	RatedCount       int                `json:"rated_count"`
}

// topN caps TopAuthors and TopShelves. Eight covers any realistic
// "favourite authors" signal for the S18 AuthorAffinity/ShelfSimilarity
// scorers without dragging long-tail noise into the profile.
const topN = 8

// halflifeDays parameterizes the recency decay. One year gives current
// reading recent emphasis without discarding multi-year history
// entirely. Must stay > 0.
const halflifeDays = 365.0

// weightedVal carries a (value, weight) pair through the two statistical
// helpers so both TopN scoring and mean/stdev share one shape.
type weightedVal struct {
	value  float64
	weight float64
}

// Extract is the production entry point: reads every book from the
// index, then delegates to Build. Returns an empty (non-nil) profile
// when the library is empty.
func Extract(ctx context.Context, st *store.Store) (*Profile, error) {
	books, err := st.ListBooks(ctx, store.Filter{})
	if err != nil {
		return nil, err
	}
	return Build(books, time.Now().UTC()), nil
}

// Build is the pure, deterministic profile computation. Exposed
// separately so unit tests can pin "now" and avoid spinning up a real
// store. Returns a non-nil *Profile even when books is empty so
// downstream JSON encoding stays stable.
func Build(books []store.BookRow, now time.Time) *Profile {
	p := &Profile{
		AxisMeans:  map[string]float64{},
		AxisStdevs: map[string]float64{},
		BookCount:  len(books),
	}

	var (
		authorScores = map[string]float64{}
		shelfScores  = map[string]float64{}
		axisValues   = map[string][]weightedVal{}
		lengthValues []weightedVal
		ratingValues []weightedVal
	)

	for _, b := range books {
		rec := recencyWeight(b, now)
		if b.RatingOverall == nil {
			continue
		}
		p.RatedCount++
		rating := *b.RatingOverall
		score := rating * rec
		for _, a := range b.Authors {
			if a == "" {
				continue
			}
			authorScores[a] += score
		}
		for _, c := range b.Categories {
			if c == "" {
				continue
			}
			shelfScores[c] += score
		}
		ratingValues = append(ratingValues, weightedVal{value: rating, weight: rec})

		if b.TotalPages != nil && *b.TotalPages > 0 {
			lengthValues = append(lengthValues, weightedVal{
				value:  float64(*b.TotalPages),
				weight: rating * rec,
			})
		}

		for axis, v := range b.RatingDimensions {
			if axis == "" {
				continue
			}
			axisValues[axis] = append(axisValues[axis], weightedVal{
				value:  float64(v),
				weight: rec,
			})
		}
	}

	p.TopAuthors = topKeysByScore(authorScores, topN)
	p.TopShelves = topKeysByScore(shelfScores, topN)

	for axis, samples := range axisValues {
		mean, ok := weightedMean(samples)
		if !ok {
			continue
		}
		p.AxisMeans[axis] = mean
		if len(samples) >= 2 {
			if stdev, ok := weightedStdev(samples, mean); ok {
				p.AxisStdevs[axis] = stdev
			}
		}
	}

	if mean, ok := weightedMean(lengthValues); ok {
		p.LengthMean = mean
		if len(lengthValues) >= 2 {
			if stdev, ok := weightedStdev(lengthValues, mean); ok {
				p.LengthStdev = &stdev
			}
		}
	}

	if mean, ok := weightedMean(ratingValues); ok {
		p.RatingMean = mean
	}

	p.SeriesInProgress = seriesInProgress(books)
	return p
}

// recencyWeight returns 2^(-Δt / halflife) where Δt is the gap between
// now and the most recent parseable ISO date on the book (FinishedDates
// first, StartedDates fallback). Books with no parseable dates return
// weight 1.0 so pre-dates-tracking notes still contribute fully. Future
// dates clamp Δt to 0 — weight stays 1.0.
func recencyWeight(b store.BookRow, now time.Time) float64 {
	date, ok := latestValidDate(b.FinishedDates)
	if !ok {
		date, ok = latestValidDate(b.StartedDates)
	}
	if !ok {
		return 1.0
	}
	deltaDays := now.Sub(date).Hours() / 24.0
	if deltaDays < 0 {
		deltaDays = 0
	}
	return math.Pow(2, -deltaDays/halflifeDays)
}

// latestValidDate returns the chronologically latest parseable YYYY-MM-DD
// entry in the slice. Entries that fail to parse are silently skipped —
// the caller has no cheap way to surface them, and a single malformed
// date must not break Extract.
func latestValidDate(dates []string) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, s := range dates {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			continue
		}
		if !found || t.After(latest) {
			latest = t
			found = true
		}
	}
	return latest, found
}

// topKeysByScore extracts the top-k map keys by descending score. Ties
// break alphabetically to keep output deterministic across runs. Zero
// or negative scores are excluded — a 0-rated author tells us nothing.
func topKeysByScore(scores map[string]float64, k int) []string {
	if len(scores) == 0 {
		return nil
	}
	keys := make([]string, 0, len(scores))
	for kk, v := range scores {
		if v > 0 {
			keys = append(keys, kk)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Slice(keys, func(i, j int) bool {
		if scores[keys[i]] != scores[keys[j]] {
			return scores[keys[i]] > scores[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > k {
		keys = keys[:k]
	}
	return keys
}

// weightedMean returns Σ(w·v) / Σw. Returns (0, false) if the sample
// list is empty or the combined weight is zero (which can happen if
// every rating is zero — a pathological but defensible edge case).
func weightedMean(samples []weightedVal) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	var num, den float64
	for _, s := range samples {
		num += s.value * s.weight
		den += s.weight
	}
	if den == 0 {
		return 0, false
	}
	return num / den, true
}

// weightedStdev returns the population weighted stdev around the
// supplied mean: sqrt(Σw·(v−μ)² / Σw). Caller guarantees at least two
// samples before invoking. Returns (0, false) when total weight is zero.
func weightedStdev(samples []weightedVal, mean float64) (float64, bool) {
	var num, den float64
	for _, s := range samples {
		d := s.value - mean
		num += s.weight * d * d
		den += s.weight
	}
	if den == 0 {
		return 0, false
	}
	return math.Sqrt(num / den), true
}

// seriesInProgress picks out series where the user has at least one
// book but hasn't closed every known gap. Names are sorted ascending.
// S18's SeriesCompletion scorer will pair this with the Detect output
// to boost candidates whose series_index falls in a gap.
func seriesInProgress(books []store.BookRow) []string {
	states := series.Detect(books)
	var out []string
	for _, s := range states {
		if s.Owned > 0 && !s.Complete {
			out = append(out, s.Name)
		}
	}
	sort.Strings(out)
	return out
}
