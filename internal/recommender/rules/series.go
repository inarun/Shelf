package rules

import (
	"fmt"
	"math"

	"github.com/inarun/Shelf/internal/domain/series"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

// ScoreSeriesCompletion returns Value 1.0 + a "Continues {Name}" reason
// when the candidate's floored series_index falls in the gap list of a
// matching series.State. Floors mirror series.Detect's own floor pass —
// a candidate at index 1.5 in a series with Gap=1 *will* match.
//
// Returns Score{} when the candidate has no series, no SeriesIndex, no
// matching state, or its index isn't in any gap. p is unused today but
// retained for symmetry with the other scorers (and for v0.5 LLM
// tuning of, e.g., per-series-affinity weighting).
func ScoreSeriesCompletion(b store.BookRow, p *profile.Profile, ss []series.State) Score {
	_ = p
	if b.SeriesName == "" || b.SeriesIndex == nil {
		return Score{}
	}
	floor := int(math.Floor(*b.SeriesIndex))
	for _, s := range ss {
		if s.Name != b.SeriesName {
			continue
		}
		for _, g := range s.Gaps {
			if g == floor {
				return Score{
					Value:  1.0,
					Reason: fmt.Sprintf("Continues %s (book %d of %d)", s.Name, floor, s.Total),
				}
			}
		}
	}
	return Score{}
}
