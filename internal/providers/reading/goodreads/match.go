package goodreads

import (
	"context"
	"fmt"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/strmatch"
)

// MatchResult describes how a Record matches an existing vault note.
type MatchResult struct {
	// Filename of the matched note; empty when no match found.
	Filename string
	// Reason is a short human-readable description of why the match
	// succeeded ("ISBN13 match", "fuzzy title+author match (ratio=0.97)",
	// etc.).
	Reason string
	// Fuzzy reports whether the match came from the Levenshtein fallback
	// rather than an exact ISBN lookup.
	Fuzzy bool
	// NeedsUserDecision marks borderline matches the planner must surface
	// as conflicts. True when the fuzzy ratio is in [0.80, 0.92), when
	// the fuzzy ratio is ≥ 0.92 but surnames differ, or when multiple
	// candidates share the top fuzzy score.
	NeedsUserDecision bool
}

// Resolver caches a normalized index of existing vault notes so Match
// runs in O(1) for ISBN lookups and O(N) for the fuzzy fallback, where
// N is the number of books in the vault.
type Resolver struct {
	byISBN13      map[string]string
	byISBN10      map[string]string
	byTitleAuthor map[titleAuthorKey]string
	normalized    []normalizedRow
}

type titleAuthorKey struct {
	Title   string
	Surname string
}

type normalizedRow struct {
	Filename  string
	NormTitle string
	Surname   string
}

// Fuzzy thresholds per SKILL.md §Goodreads.
const (
	fuzzyAutoMatch  = 0.92
	fuzzyConflict   = 0.80
)

// NewResolver loads every book from the store and builds the lookup
// index. The resolver is a point-in-time snapshot; a vault changing
// while a Plan is being applied invalidates the cached map. That's the
// same window the Apply path already defends against via per-entry
// staleness checks.
func NewResolver(ctx context.Context, s *store.Store) (*Resolver, error) {
	books, err := s.ListBooks(ctx, store.Filter{})
	if err != nil {
		return nil, fmt.Errorf("goodreads: loading resolver: %w", err)
	}
	rv := &Resolver{
		byISBN13:      map[string]string{},
		byISBN10:      map[string]string{},
		byTitleAuthor: map[titleAuthorKey]string{},
	}
	for _, b := range books {
		if b.ISBN != "" {
			// The vault stores ISBN in whatever form the frontmatter
			// carries; normalize for comparison.
			normalized := normalizeISBN(b.ISBN)
			switch len(normalized) {
			case 13:
				rv.byISBN13[normalized] = b.Filename
			case 10:
				rv.byISBN10[normalized] = b.Filename
			}
		}
		normTitle := strmatch.Normalize(b.Title)
		var surname string
		if len(b.Authors) > 0 {
			surname = strmatch.Surname(b.Authors[0])
		}
		if normTitle != "" && surname != "" {
			rv.byTitleAuthor[titleAuthorKey{Title: normTitle, Surname: surname}] = b.Filename
		}
		if normTitle != "" {
			rv.normalized = append(rv.normalized, normalizedRow{
				Filename:  b.Filename,
				NormTitle: normTitle,
				Surname:   surname,
			})
		}
	}
	return rv, nil
}

// Match looks up an existing vault note for r. The priority order
// mirrors SKILL.md §Goodreads CSV import: ISBN13 → ISBN10 → fuzzy
// title+author. Returns (MatchResult, true) when a candidate is found
// (match may still require user decision); (zero, false) when no match.
func (rv *Resolver) Match(r Record) (MatchResult, bool) {
	if r.ISBN13 != "" {
		if fn, ok := rv.byISBN13[r.ISBN13]; ok {
			return MatchResult{Filename: fn, Reason: "ISBN13 match"}, true
		}
	}
	if r.ISBN10 != "" {
		if fn, ok := rv.byISBN10[r.ISBN10]; ok {
			return MatchResult{Filename: fn, Reason: "ISBN10 match"}, true
		}
	}

	if r.Title == "" || len(r.Authors) == 0 {
		return MatchResult{}, false
	}
	normTitle := strmatch.Normalize(r.Title)
	surname := strmatch.Surname(r.Authors[0])
	if normTitle == "" || surname == "" {
		return MatchResult{}, false
	}

	// Exact normalized hit is treated as auto-match with ratio 1.0.
	if fn, ok := rv.byTitleAuthor[titleAuthorKey{Title: normTitle, Surname: surname}]; ok {
		return MatchResult{
			Filename: fn,
			Reason:   "fuzzy title+author match (ratio=1.00)",
			Fuzzy:    true,
		}, true
	}

	// Compute the single best fuzzy candidate plus whether a second
	// candidate tied for auto-threshold.
	var (
		best       *normalizedRow
		bestRatio  float64
		tiedAtAuto bool
	)
	for i := range rv.normalized {
		row := &rv.normalized[i]
		if row.NormTitle == "" {
			continue
		}
		ratio := strmatch.Ratio(normTitle, row.NormTitle)
		if ratio < fuzzyConflict {
			continue
		}
		if best == nil || ratio > bestRatio {
			best = row
			bestRatio = ratio
			tiedAtAuto = false
			continue
		}
		if ratio == bestRatio && bestRatio >= fuzzyAutoMatch {
			tiedAtAuto = true
		}
	}
	if best == nil {
		return MatchResult{}, false
	}

	result := MatchResult{
		Filename: best.Filename,
		Reason:   fmt.Sprintf("fuzzy title+author match (ratio=%.2f)", bestRatio),
		Fuzzy:    true,
	}
	switch {
	case bestRatio >= fuzzyAutoMatch && best.Surname == surname && !tiedAtAuto:
		// Auto-match.
	case bestRatio >= fuzzyAutoMatch && best.Surname != surname:
		result.NeedsUserDecision = true
		result.Reason = fmt.Sprintf("title fuzzy match (ratio=%.2f) but authors differ", bestRatio)
	case tiedAtAuto:
		result.NeedsUserDecision = true
		result.Reason = fmt.Sprintf("multiple notes could match (ratio=%.2f)", bestRatio)
	default:
		result.NeedsUserDecision = true
	}
	return result, true
}
