package audiobookshelf

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inarun/Shelf/internal/domain/precedence"
	"github.com/inarun/Shelf/internal/domain/timeline"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/strmatch"
)

// Fuzzy thresholds — same three-band scheme as the Goodreads importer
// (see internal/providers/reading/goodreads/match.go). Duplicated here
// rather than imported because goodreads owns its own private resolver
// shape; lifting a shared matcher to internal/providers/reading/matching
// is scheduled for v0.4 (Kavita) when a third consumer lands.
const (
	fuzzyAutoMatch = 0.92
	fuzzyConflict  = 0.80
)

// TODO(post-v0.2): enable ASIN matching once SKILL.md §Frontmatter
// schema adds an `asin` key. Until then, the matcher falls back from
// ISBN to fuzzy title+author without an ASIN band.

// MatchResult describes how a LibraryItem matches an existing vault
// note. Mirrors goodreads.MatchResult so the surrounding plan/apply
// logic can stay near-identical between providers.
type MatchResult struct {
	Filename          string
	Reason            string
	Fuzzy             bool
	NeedsUserDecision bool
}

// Resolver is a point-in-time index of vault notes keyed by ISBN and by
// normalized title+surname. Built once per BuildPlan run; stale once the
// vault changes (Apply guards against drift via the staleness pair).
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

// NewResolver loads every book from the store and builds the lookup
// index. Same shape as goodreads.NewResolver.
func NewResolver(ctx context.Context, s *store.Store) (*Resolver, error) {
	books, err := s.ListBooks(ctx, store.Filter{})
	if err != nil {
		return nil, fmt.Errorf("audiobookshelf: loading resolver: %w", err)
	}
	rv := &Resolver{
		byISBN13:      map[string]string{},
		byISBN10:      map[string]string{},
		byTitleAuthor: map[titleAuthorKey]string{},
	}
	for _, b := range books {
		if b.ISBN != "" {
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

// Match looks up an existing vault note for item. Priority: ISBN13 →
// ISBN10 → exact normalized title+author → fuzzy title+author.
// Returns (result, true) when a candidate is found (may still require
// user decision); (zero, false) when no candidate qualifies.
func (rv *Resolver) Match(item LibraryItem) (MatchResult, bool) {
	isbn := normalizeISBN(item.Media.Metadata.ISBN)
	if len(isbn) == 13 {
		if fn, ok := rv.byISBN13[isbn]; ok {
			return MatchResult{Filename: fn, Reason: "ISBN13 match"}, true
		}
	}
	if len(isbn) == 10 {
		if fn, ok := rv.byISBN10[isbn]; ok {
			return MatchResult{Filename: fn, Reason: "ISBN10 match"}, true
		}
	}

	title := item.Media.Metadata.Title
	author := firstAuthor(item.Media.Metadata.AuthorName)
	if title == "" || author == "" {
		return MatchResult{}, false
	}
	normTitle := strmatch.Normalize(title)
	surname := strmatch.Surname(author)
	if normTitle == "" || surname == "" {
		return MatchResult{}, false
	}
	if fn, ok := rv.byTitleAuthor[titleAuthorKey{Title: normTitle, Surname: surname}]; ok {
		return MatchResult{
			Filename: fn,
			Reason:   "fuzzy title+author match (ratio=1.00)",
			Fuzzy:    true,
		}, true
	}

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

// ItemToEntries produces at most one aggregated timeline.Entry per
// item, built from the item's associated listening sessions. Granularity
// decision (2026-04-19): one Entry per book, not one per session —
// otherwise a 30-session audiobook would spam 30 "## Reading Timeline"
// lines and overflow the frontmatter started[]/finished[] pairs.
//
//	Start      = earliest session.StartedAt for this LibraryItemID
//	End        = latest session.UpdatedAt when item.IsFinished, else zero
//	Kind       = KindFinished when item.IsFinished, else KindProgress
//	ExternalID = item.ID (the library item id, not a session id)
//	Source     = precedence.SourceAudiobookshelf
//
// Sessions that don't reference this item are ignored. Returns nil when
// no session touches the item and item.LastUpdate is zero (no activity).
func ItemToEntries(item LibraryItem, sessions []ListeningSession) []timeline.Entry {
	var (
		earliest int64 = 0 // ms since epoch; 0 ⇒ unset
		latest   int64 = 0
	)
	for _, s := range sessions {
		if s.LibraryItemID != item.ID {
			continue
		}
		if s.StartedAt > 0 && (earliest == 0 || s.StartedAt < earliest) {
			earliest = s.StartedAt
		}
		if s.UpdatedAt > latest {
			latest = s.UpdatedAt
		}
	}
	// Fall back to the item's LastUpdate if no matching sessions — some
	// AB deployments (or older items) carry no session rows but still
	// report IsFinished.
	if earliest == 0 && item.LastUpdate > 0 {
		earliest = item.LastUpdate
	}
	if latest == 0 && item.LastUpdate > 0 {
		latest = item.LastUpdate
	}
	if earliest == 0 {
		return nil
	}

	entry := timeline.Entry{
		ExternalID: item.ID,
		Source:     precedence.SourceAudiobookshelf,
		Start:      time.UnixMilli(earliest).UTC(),
	}
	if item.IsFinished {
		entry.Kind = timeline.KindFinished
		if latest > 0 {
			entry.End = time.UnixMilli(latest).UTC()
		} else {
			entry.End = entry.Start
		}
		entry.Note = "Finished listening (Audiobookshelf)"
	} else {
		entry.Kind = timeline.KindProgress
		entry.Note = "Started listening (Audiobookshelf)"
	}
	return []timeline.Entry{entry}
}

// VaultEntriesFromFrontmatter builds timeline.Entry values from a
// note's frontmatter started[]/finished[] arrays. Each paired index
// becomes one Entry with Source = SourceVaultFrontmatter. Unfinished
// trailing started[] entries produce an ongoing (End zero) Entry.
func VaultEntriesFromFrontmatter(started, finished []time.Time) []timeline.Entry {
	out := make([]timeline.Entry, 0, len(started))
	for i, s := range started {
		e := timeline.Entry{
			Source: precedence.SourceVaultFrontmatter,
			Start:  s.UTC(),
			Kind:   timeline.KindFinished,
		}
		if i < len(finished) {
			e.End = finished[i].UTC()
		} else {
			// Ongoing or paused — leave End zero; Merge treats it as a
			// point-in-time event, not an open-ended range.
			e.End = time.Time{}
			e.Kind = timeline.KindProgress
		}
		out = append(out, e)
	}
	return out
}

// firstAuthor returns the first comma-separated token of AB's
// authorName display string. AB renders multi-author works as
// "Name A, Name B, Name C"; we only need one for surname fuzzy matching.
func firstAuthor(authorName string) string {
	if authorName == "" {
		return ""
	}
	if idx := strings.Index(authorName, ","); idx >= 0 {
		return strings.TrimSpace(authorName[:idx])
	}
	return strings.TrimSpace(authorName)
}

// normalizeISBN strips common punctuation from an ISBN string and
// uppercases. Callers verify the resulting length (10 or 13) to decide
// which lookup bucket to hit.
func normalizeISBN(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'X' || r == 'x':
			b.WriteRune('X')
		}
	}
	return b.String()
}

