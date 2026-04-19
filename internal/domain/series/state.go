package series

import (
	"math"
	"sort"

	"github.com/inarun/Shelf/internal/index/store"
)

// State summarizes what Shelf knows about one series based solely on
// the indexed books the user owns. All fields are derived — no external
// catalog (Hardcover, Goodreads) is consulted. Total is therefore the
// greatest integer series_index observed, which lets the S18
// SeriesCompletion scorer boost candidates that fill Gaps but cannot
// claim to know the *true* series length.
//
// A book with SeriesName set and SeriesIndex == nil (e.g., an indexed
// note the user hasn't yet labeled with its position) contributes to
// Owned only; it does not move Total or MaxOwnedIndex, and it does not
// close gaps. Complete is deliberately derived from Gaps alone for the
// same reason — nil-index books cannot prove series-completeness.
type State struct {
	Name          string  `json:"name"`
	Total         int     `json:"total"`
	Owned         int     `json:"owned"`
	MaxOwnedIndex float64 `json:"max_owned_index"`
	Gaps          []int   `json:"gaps"`
	Complete      bool    `json:"complete"`
}

// Detect groups the supplied book rows by SeriesName and returns one
// State per non-empty series, sorted ascending by Name. Books with an
// empty SeriesName are skipped. Output is deterministic: Gaps are
// always ascending; the outer slice is alphabetically ordered.
func Detect(books []store.BookRow) []State {
	if len(books) == 0 {
		return nil
	}

	type accum struct {
		owned         int
		maxOwnedIndex float64
		maxFloor      int
		covered       map[int]bool
	}
	groups := map[string]*accum{}

	for _, b := range books {
		if b.SeriesName == "" {
			continue
		}
		g, ok := groups[b.SeriesName]
		if !ok {
			g = &accum{covered: map[int]bool{}}
			groups[b.SeriesName] = g
		}
		g.owned++
		if b.SeriesIndex == nil {
			continue
		}
		idx := *b.SeriesIndex
		if idx > g.maxOwnedIndex {
			g.maxOwnedIndex = idx
		}
		floor := int(math.Floor(idx))
		if floor >= 1 {
			g.covered[floor] = true
			if floor > g.maxFloor {
				g.maxFloor = floor
			}
		}
	}

	if len(groups) == 0 {
		return nil
	}

	out := make([]State, 0, len(groups))
	for name, g := range groups {
		total := g.maxFloor
		var gaps []int
		for i := 1; i <= total; i++ {
			if !g.covered[i] {
				gaps = append(gaps, i)
			}
		}
		out = append(out, State{
			Name:          name,
			Total:         total,
			Owned:         g.owned,
			MaxOwnedIndex: g.maxOwnedIndex,
			Gaps:          gaps,
			Complete:      total > 0 && len(gaps) == 0,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
