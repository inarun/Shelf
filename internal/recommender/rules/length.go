package rules

import (
	"fmt"
	"math"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

// lengthReasonThreshold is the Value floor below which the reason is
// suppressed. The Gaussian preserves a continuous score regardless, so
// the combined ranker still uses the numeric contribution — we just
// don't surface "around your usual length" to the user when the match
// is weaker than ~one stdev. 0.5 corresponds to ~1.18σ off mean.
const lengthReasonThreshold = 0.5

// ScoreLengthMatch returns a Gaussian decay around p.LengthMean with
// σ = *p.LengthStdev: exp(-(pages - mean)² / (2σ²)). The Value falls
// continuously from 1.0 at the mean.
//
// Returns Score{} when:
//   - candidate.TotalPages is nil (length unknown — no signal),
//   - p.LengthStdev is nil (fewer than 2 length samples — graceful
//     degradation per the same rule that suppresses AxisStdevs keys), or
//   - p.LengthMean == 0 / σ <= 0 (defensive).
//
// Reason is emitted only when Value > lengthReasonThreshold.
func ScoreLengthMatch(b store.BookRow, p *profile.Profile) Score {
	if b.TotalPages == nil || p.LengthStdev == nil || p.LengthMean == 0 {
		return Score{}
	}
	sigma := *p.LengthStdev
	if sigma <= 0 {
		return Score{}
	}
	diff := float64(*b.TotalPages) - p.LengthMean
	v := math.Exp(-(diff * diff) / (2 * sigma * sigma))
	if v <= lengthReasonThreshold {
		return Score{Value: v}
	}
	return Score{
		Value:  v,
		Reason: fmt.Sprintf("Around your usual length (~%dp)", int(math.Round(p.LengthMean))),
	}
}
