package rules

import (
	"fmt"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/recommender/profile"
)

// ScoreAuthorAffinity returns Value 1.0 + a reason naming the matched
// author when any of b.Authors appears in p.TopAuthors. First match
// wins (BookRow.Authors is position-ordered); multiple matches don't
// raise the score above 1.0 because the signal is "user reads this
// author" — not "user reads this author multiple times".
//
// Returns Score{} when TopAuthors is empty (fresh user) or no overlap.
func ScoreAuthorAffinity(b store.BookRow, p *profile.Profile) Score {
	if len(p.TopAuthors) == 0 {
		return Score{}
	}
	top := make(map[string]struct{}, len(p.TopAuthors))
	for _, a := range p.TopAuthors {
		top[a] = struct{}{}
	}
	for _, a := range b.Authors {
		if _, ok := top[a]; ok {
			return Score{Value: 1.0, Reason: fmt.Sprintf("By %s, who you read often", a)}
		}
	}
	return Score{}
}
