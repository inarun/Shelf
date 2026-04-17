package goodreads

import (
	"strings"

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

	if len(fm.Authors()) == 0 && len(r.Authors) > 0 {
		changes = append(changes, FieldChange{Field: frontmatter.KeyAuthors, Old: nil, New: r.Authors})
	}
	if len(fm.Categories()) == 0 && len(r.Bookshelves) > 0 {
		changes = append(changes, FieldChange{Field: frontmatter.KeyCategories, Old: nil, New: r.Bookshelves})
	}
	if fm.ISBN() == "" {
		switch {
		case r.ISBN13 != "":
			changes = append(changes, FieldChange{Field: frontmatter.KeyISBN, Old: "", New: r.ISBN13})
		case r.ISBN10 != "":
			changes = append(changes, FieldChange{Field: frontmatter.KeyISBN, Old: "", New: r.ISBN10})
		}
	}
	if fm.Publisher() == "" && r.Publisher != "" {
		changes = append(changes, FieldChange{Field: frontmatter.KeyPublisher, Old: "", New: r.Publisher})
	}
	if fm.Publish() == "" && r.YearPublished != "" {
		changes = append(changes, FieldChange{Field: frontmatter.KeyPublish, Old: "", New: r.YearPublished})
	}
	if fm.TotalPages() == nil && r.TotalPages != nil {
		changes = append(changes, FieldChange{Field: frontmatter.KeyTotalPages, Old: nil, New: *r.TotalPages})
	}
	if fm.Subtitle() == "" && r.Subtitle != "" {
		changes = append(changes, FieldChange{Field: frontmatter.KeySubtitle, Old: "", New: r.Subtitle})
	}
	if fm.Series() == "" && r.Series != "" {
		changes = append(changes, FieldChange{Field: frontmatter.KeySeries, Old: "", New: r.Series})
	}
	if fm.SeriesIndex() == nil && r.SeriesIndex != nil {
		changes = append(changes, FieldChange{Field: frontmatter.KeySeriesIndex, Old: nil, New: *r.SeriesIndex})
	}
	if fm.Rating() == nil && r.MyRating > 0 {
		changes = append(changes, FieldChange{Field: frontmatter.KeyRating, Old: nil, New: r.MyRating})
	}
	// Status with the template-default exception: an existing "unread"
	// value is treated as a gap.
	currentStatus := fm.Status()
	if (currentStatus == "" || currentStatus == "unread") && r.Status != "" && r.Status != currentStatus {
		changes = append(changes, FieldChange{Field: frontmatter.KeyStatus, Old: currentStatus, New: r.Status})
	}
	// Finished array: fill only if empty and CSV says the shelf is
	// finished with a Date Read.
	if len(fm.Finished()) == 0 && r.Status == "finished" && r.DateRead != nil {
		changes = append(changes, FieldChange{
			Field: frontmatter.KeyFinished,
			Old:   nil,
			New:   []string{r.DateRead.Format("2006-01-02")},
		})
		// Bump read_count if we're setting finished from empty.
		existing := fm.ReadCount()
		if existing < 1 {
			changes = append(changes, FieldChange{Field: frontmatter.KeyReadCount, Old: existing, New: 1})
		}
	}
	if strings.TrimSpace(r.Review) != "" && !hasImportedReview(n) {
		changes = append(changes, FieldChange{Field: "body.notes", Old: nil, New: r.Review})
	}

	return changes
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
