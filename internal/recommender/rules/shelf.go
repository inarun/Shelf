package rules

import (
	"sort"
	"strings"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

// ScoreShelfSimilarity returns Jaccard |A ∩ B| / |A ∪ B| over the sets
// of candidate Categories and Profile.TopShelves. Penalizes off-shelf
// categories (a sci-fi book also tagged with five obscure niche shelves
// scores lower than a focused sci-fi book), making this a "how focused
// a match" signal — orthogonal to GenreMatch's "any-hit coverage".
//
// Reason (when Value > 0): "Matches your top shelves: X" or "...: X, Y"
// (top two overlap in alphabetical order for stability across runs).
// Returns Score{} when either set is empty or no overlap exists.
func ScoreShelfSimilarity(b store.BookRow, p *profile.Profile) Score {
	if len(b.Categories) == 0 || len(p.TopShelves) == 0 {
		return Score{}
	}
	a := stringSet(b.Categories)
	tops := stringSet(p.TopShelves)
	inter := intersectSorted(a, tops)
	if len(inter) == 0 {
		return Score{}
	}
	union := len(a) + len(tops) - len(inter)
	if union == 0 {
		return Score{}
	}
	v := float64(len(inter)) / float64(union)
	show := inter
	if len(show) > 2 {
		show = show[:2]
	}
	return Score{
		Value:  v,
		Reason: "Matches your top shelves: " + strings.Join(show, ", "),
	}
}

// ScoreGenreMatch returns |A ∩ B| / max(1, |TopShelves|) clamped to
// [0, 1]. Soft "did this hit any of your top genres" coverage signal —
// distinct from ShelfSimilarity in that the denominator only depends on
// the user's profile, not the candidate's tagging breadth. Capped at
// 1.0 because |∩| can equal |TopShelves| (every top shelf appears).
//
// Reason: "On your top shelf: X" when exactly one overlap (singular);
// "On your top shelves: X, Y, Z" otherwise (capped at three shown).
// Returns Score{} when either set is empty or no overlap.
func ScoreGenreMatch(b store.BookRow, p *profile.Profile) Score {
	if len(b.Categories) == 0 || len(p.TopShelves) == 0 {
		return Score{}
	}
	a := stringSet(b.Categories)
	tops := stringSet(p.TopShelves)
	inter := intersectSorted(a, tops)
	if len(inter) == 0 {
		return Score{}
	}
	v := float64(len(inter)) / float64(len(tops))
	if v > 1.0 {
		v = 1.0
	}
	var reason string
	if len(inter) == 1 {
		reason = "On your top shelf: " + inter[0]
	} else {
		show := inter
		if len(show) > 3 {
			show = show[:3]
		}
		reason = "On your top shelves: " + strings.Join(show, ", ")
	}
	return Score{Value: v, Reason: reason}
}

// stringSet builds a set from a slice, dropping empties so empty
// frontmatter entries don't poison overlap counts.
func stringSet(xs []string) map[string]struct{} {
	out := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		if x == "" {
			continue
		}
		out[x] = struct{}{}
	}
	return out
}

// intersectSorted returns the alphabetically-sorted intersection of two
// sets. Sort is up-front so callers don't have to re-sort to surface
// deterministic reason strings.
func intersectSorted(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
