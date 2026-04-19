package goodreads

import (
	"strings"
	"time"

	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// buildNewNote constructs a Frontmatter + Body for a CSV Record destined
// for a brand-new vault note. The frontmatter layout matches the
// Obsidian Book Search plugin template (tag 📚Book, empty strings for
// unfilled fields, started/finished as arrays). The body is a minimal
// H1 with the title, optional Rating line, and optional Notes section
// for the review (blockquote-wrapped to neutralize any stray `##`).
//
// importStamp is the provenance-line timestamp: every imported review
// gets a "_Imported from Goodreads on YYYY-MM-DD_" header line.
func buildNewNote(r Record, importStamp time.Time) (*frontmatter.Frontmatter, *body.Body, error) {
	fm := frontmatter.NewEmpty()
	fm.SetTag("📚Book")
	fm.SetTitle(r.Title)
	fm.SetSubtitle(r.Subtitle)
	fm.SetAuthors(r.Authors)
	fm.SetCategories(r.Bookshelves)
	fm.SetSeries(r.Series)
	if err := fm.SetSeriesIndex(r.SeriesIndex); err != nil {
		return nil, nil, err
	}
	fm.SetPublisher(r.Publisher)
	fm.SetPublish(r.YearPublished)
	if err := fm.SetTotalPages(r.TotalPages); err != nil {
		return nil, nil, err
	}
	switch {
	case r.ISBN13 != "":
		fm.SetISBN(r.ISBN13)
	case r.ISBN10 != "":
		fm.SetISBN(r.ISBN10)
	}
	fm.SetCover("")
	// Format is intentionally left null for v0.1 — Goodreads doesn't
	// reliably carry this and the user prefers explicit over guessed.
	fm.SetSource("")

	// Started is always an empty array on import — Goodreads has no
	// start date.
	fm.SetAuthors(r.Authors) // (re-set OK; keeps both if first call was nil)

	// Rating (nullable). Goodreads only provides an overall score, so
	// TrialSystem stays empty — the dimensioned axes remain for the user
	// to fill in post-v0.2.1.
	if r.MyRating > 0 {
		over := float64(r.MyRating)
		if err := fm.SetRating(&frontmatter.Rating{Overall: &over}); err != nil {
			return nil, nil, err
		}
	}

	// Status with re-read bookkeeping.
	status := r.Status
	if status == "" {
		status = "unread"
	}
	if err := fm.SetStatus(status); err != nil {
		return nil, nil, err
	}
	if status == "finished" && r.DateRead != nil {
		fm.AppendFinished(*r.DateRead)
		fm.SetReadCount(1)
	}

	// Body: title + optional Rating section (dual-written from
	// frontmatter) + optional Notes with review.
	bd := &body.Body{}
	bd.SetTitle(r.Title)
	if rating := fm.Rating(); rating != nil {
		bd.SetRatingFromFrontmatter(rating)
	}
	if strings.TrimSpace(r.Review) != "" {
		bd.SetNotes(composeReviewBody(r.Review, importStamp))
	}
	if r.DateAdded != nil {
		bd.AppendTimelineEvent(*r.DateAdded, "Added to shelf")
	}
	return fm, bd, nil
}

// composeReviewBody formats review text for the ## Notes section: the
// provenance line on top, then every review line prefixed with "> " so
// a line that starts with "## " can never be misread as a new section
// heading by the body parser.
func composeReviewBody(review string, importStamp time.Time) string {
	var b strings.Builder
	b.WriteString("_Imported from Goodreads on ")
	b.WriteString(importStamp.Format("2006-01-02"))
	b.WriteString("_\n\n")
	for _, line := range strings.Split(review, "\n") {
		// Strip stray CR to keep the blockquote lines clean.
		line = strings.TrimRight(line, "\r")
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
