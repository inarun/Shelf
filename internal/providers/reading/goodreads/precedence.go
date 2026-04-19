package goodreads

import (
	"strings"

	"github.com/inarun/Shelf/internal/domain/precedence"
	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
)

// computeChanges applies SKILL.md §Data precedence to produce the set
// of field mutations Apply should perform on an existing note. External
// sources (here: Goodreads) fill gaps, never overwrite — with the
// template-default exception for `status: unread`.
//
// The returned FieldChange list uses "body.notes" as the Field name for
// a review insertion; all other Field names match the frontmatter key.
func computeChanges(r Record, n *note.Note) []FieldChange {
	var changes []FieldChange
	fm := n.Frontmatter

	if w, ok := precedence.Resolve([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.Authors()},
		{Source: precedence.SourceGoodreads, Value: r.Authors},
	}); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeyAuthors, Old: nil, New: w.Value})
	}
	if w, ok := precedence.Resolve([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.Categories()},
		{Source: precedence.SourceGoodreads, Value: r.Bookshelves},
	}); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeyCategories, Old: nil, New: w.Value})
	}

	// ISBN: vault populated → keep; else prefer ISBN13 over ISBN10.
	if precedence.IsGap(fm.ISBN()) {
		switch {
		case r.ISBN13 != "":
			changes = append(changes, FieldChange{Field: frontmatter.KeyISBN, Old: "", New: r.ISBN13})
		case r.ISBN10 != "":
			changes = append(changes, FieldChange{Field: frontmatter.KeyISBN, Old: "", New: r.ISBN10})
		}
	}

	if w, ok := precedence.Resolve([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.Publisher()},
		{Source: precedence.SourceGoodreads, Value: r.Publisher},
	}); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeyPublisher, Old: "", New: w.Value})
	}
	if w, ok := precedence.Resolve([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.Publish()},
		{Source: precedence.SourceGoodreads, Value: r.YearPublished},
	}); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeyPublish, Old: "", New: w.Value})
	}
	if w, ok := precedence.Resolve([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.TotalPages()},
		{Source: precedence.SourceGoodreads, Value: r.TotalPages},
	}); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeyTotalPages, Old: nil, New: *r.TotalPages})
	}
	if w, ok := precedence.Resolve([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.Subtitle()},
		{Source: precedence.SourceGoodreads, Value: r.Subtitle},
	}); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeySubtitle, Old: "", New: w.Value})
	}
	if w, ok := precedence.Resolve([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.Series()},
		{Source: precedence.SourceGoodreads, Value: r.Series},
	}); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeySeries, Old: "", New: w.Value})
	}
	if w, ok := precedence.Resolve([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.SeriesIndex()},
		{Source: precedence.SourceGoodreads, Value: r.SeriesIndex},
	}); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeySeriesIndex, Old: nil, New: *r.SeriesIndex})
	}

	// Ratings post-v0.2.1 carry a dimensioned shape; a Rating{} with no
	// TrialSystem and no Overall is a gap even though the pointer is
	// non-nil. Goodreads supplies overall-only ratings as Rating.Overall.
	var goodreadsRating *frontmatter.Rating
	if r.MyRating > 0 {
		over := float64(r.MyRating)
		goodreadsRating = &frontmatter.Rating{Overall: &over}
	}
	if w, ok := precedence.ResolveWith([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: fm.Rating()},
		{Source: precedence.SourceGoodreads, Value: goodreadsRating},
	}, isRatingGap); ok && w.Source == precedence.SourceGoodreads {
		changes = append(changes, FieldChange{Field: frontmatter.KeyRating, Old: nil, New: goodreadsRating})
	}

	// Status: "unread" is a gap per SKILL.md §Data precedence (Session 3
	// rule). ResolveWith + IsStatusGap handles the override.
	currentStatus := fm.Status()
	if w, ok := precedence.ResolveWith([]precedence.Candidate{
		{Source: precedence.SourceVaultFrontmatter, Value: currentStatus},
		{Source: precedence.SourceGoodreads, Value: r.Status},
	}, precedence.IsStatusGap); ok && w.Source == precedence.SourceGoodreads && r.Status != currentStatus {
		changes = append(changes, FieldChange{Field: frontmatter.KeyStatus, Old: currentStatus, New: r.Status})
	}

	// Finished array + read_count bump are co-dependent business rules
	// (only bump the count if the shelf is finished and the date is
	// present and the array is a gap). Kept inline — not a simple
	// per-field precedence decision.
	if len(fm.Finished()) == 0 && r.Status == "finished" && r.DateRead != nil {
		changes = append(changes, FieldChange{
			Field: frontmatter.KeyFinished,
			Old:   nil,
			New:   []string{r.DateRead.Format("2006-01-02")},
		})
		existing := fm.ReadCount()
		if existing < 1 {
			changes = append(changes, FieldChange{Field: frontmatter.KeyReadCount, Old: existing, New: 1})
		}
	}

	// Body-review insertion: append-only, guarded by hasImportedReview.
	// Not a frontmatter overwrite, so not routed through precedence.
	if strings.TrimSpace(r.Review) != "" && !hasImportedReview(n) {
		changes = append(changes, FieldChange{Field: "body.notes", Old: nil, New: r.Review})
	}

	return changes
}

// isRatingGap treats a nil-pointer *frontmatter.Rating and an empty
// Rating (no axes, no override) as gaps. Non-Rating types fall back to
// the general IsGap predicate so ResolveWith works for mixed candidate
// lists without surprises.
func isRatingGap(v any) bool {
	r, ok := v.(*frontmatter.Rating)
	if !ok {
		return precedence.IsGap(v)
	}
	if r == nil {
		return true
	}
	return r.IsEmpty()
}

// hasImportedReview reports whether the note's Body ## Notes section
// already contains an Imported-from-Goodreads provenance line. Used to
// avoid appending a second copy if the user re-imports the same CSV.
func hasImportedReview(n *note.Note) bool {
	for _, bl := range n.Body.Blocks {
		if bl.Kind != body.KindNotes {
			continue
		}
		if bl.Raw != nil && strings.Contains(string(bl.Raw), "_Imported from Goodreads on ") {
			return true
		}
	}
	return false
}
