package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/vault/body"
	"github.com/inarun/Shelf/internal/vault/note"
)

// LibraryIndex redirects / → /library. No-op for everything else.
func (d *Dependencies) LibraryIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/library", http.StatusFound)
}

// LibraryViewData is the template data for library.html.
type LibraryViewData struct {
	PageCommon
	Books  []store.BookRow
	Filter libraryFilter
}

type libraryFilter struct {
	Status string
	Query  string
}

// maxSearchQueryLen caps the ?q= value we round-trip into the rendered
// input. The filter is client-side only, so this is purely a
// defense-in-depth limit against pathological URL lengths; typical
// searches are a few characters.
const maxSearchQueryLen = 200

// LibraryView renders /library. Supports ?status=reading|finished|...
// and ?q= filters. Status is applied server-side via store.Filter; the
// query text is echoed back into the search input and used by the
// client-side filter in app.js (no server-side text search yet).
func (d *Dependencies) LibraryView(w http.ResponseWriter, r *http.Request) {
	fStatus := r.URL.Query().Get("status")
	filter := store.Filter{Status: fStatus}
	if fStatus != "" {
		if err := ValidateStatus(fStatus); err != nil {
			// Silently drop an invalid filter rather than fail the page —
			// status filter is a convenience, and 400-ing would be abrasive.
			filter = store.Filter{}
			fStatus = ""
		}
	}

	fQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(fQuery) > maxSearchQueryLen {
		fQuery = fQuery[:maxSearchQueryLen]
	}

	books, err := d.Store.ListBooks(r.Context(), filter)
	if err != nil {
		d.Logger.Error("library list", "err", err)
		d.renderErrorPage(w, r, http.StatusInternalServerError, "Could not load library.")
		return
	}

	d.renderHTML(w, r, "library", LibraryViewData{
		PageCommon: d.newPageCommon(r, "library"),
		Books:      books,
		Filter:     libraryFilter{Status: fStatus, Query: fQuery},
	})
}

// BookDetailData is the template data for book_detail.html.
type BookDetailData struct {
	PageCommon
	Book        BookDetailView
	Warnings    []string
	RatingRange []int
}

// BookDetailView merges the store row with the disk-only fields (review
// text + timeline lines). Embedding promotes all BookRow fields so
// templates can write {{.Book.Title}}, {{.Book.Rating}}, etc.
type BookDetailView struct {
	store.BookRow
	Review        string
	TimelineLines []string
	Timeline      []TimelineEntry
}

// TimelineEntry is one row of the structured reading-timeline block on
// the book-detail page. Built from the paired StartedDates/FinishedDates
// frontmatter arrays plus the current Status for the in-progress pivot.
// All date fields are ISO YYYY-MM-DD strings ("" when absent).
type TimelineEntry struct {
	Started  string
	Finished string
	// State is one of "finished", "reading", "paused", "dnf", or "".
	// Empty means a historical entry whose status cannot be inferred
	// (e.g. a legacy unfinished read with no terminal status).
	State string
	// Label is the pre-composed screen-reader text summarizing the
	// entry; the template uses it as the <li>'s aria-label.
	Label string
}

// BookDetail renders /books/{filename}.
func (d *Dependencies) BookDetail(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("filename")
	_, base, err := DecodeAndValidateFilename(d.BooksAbs, raw)
	if err != nil {
		d.renderErrorPage(w, r, http.StatusBadRequest, "Invalid filename: "+err.Error())
		return
	}
	row, err := d.Store.GetBookByFilename(r.Context(), base)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			d.renderErrorPage(w, r, http.StatusNotFound,
				"No book named "+base+" in the index.")
			return
		}
		d.Logger.Error("get book", "filename", base, "err", err)
		d.renderErrorPage(w, r, http.StatusInternalServerError, "Could not load book.")
		return
	}

	view, err := d.buildBookDetailView(r.Context(), row)
	if err != nil {
		d.Logger.Error("read note", "filename", base, "err", err)
		d.renderErrorPage(w, r, http.StatusInternalServerError, "Could not read note on disk.")
		return
	}

	d.renderHTML(w, r, "book_detail", BookDetailData{
		PageCommon:  d.newPageCommon(r, "library"),
		Book:        view,
		Warnings:    row.Warnings,
		RatingRange: []int{1, 2, 3, 4, 5},
	})
}

// buildBookDetailView reads the note from disk to get the review text
// (## Notes section) and the Reading Timeline entries. Factored out so
// tests can assert rendering independently of IO.
func (d *Dependencies) buildBookDetailView(ctx context.Context, row *store.BookRow) (BookDetailView, error) {
	_ = ctx // reserved for future cancellation
	abs, _, err := DecodeAndValidateFilename(d.BooksAbs, row.Filename)
	if err != nil {
		return BookDetailView{}, err
	}
	n, err := note.Read(abs)
	if err != nil {
		return BookDetailView{}, err
	}
	return BookDetailView{
		BookRow:       *row,
		Review:        n.Body.Notes(),
		TimelineLines: extractTimelineLines(n.Body),
		Timeline:      composeTimeline(row.StartedDates, row.FinishedDates, row.Status),
	}, nil
}

// composeTimeline pairs started[i] with finished[i] into TimelineEntry
// rows. For the final pair, a missing finish date is interpreted using
// the current Status (reading|paused|dnf) so the UI reflects the active
// state rather than a bare "started on X". Entries earlier in the
// sequence that are missing a finish are treated as legacy gaps and
// carry an empty State.
//
// The function is pure and deterministic for unit testing; it does not
// depend on Dependencies or the filesystem.
func composeTimeline(started, finished []string, status string) []TimelineEntry {
	n := len(started)
	if len(finished) > n {
		n = len(finished)
	}
	if n == 0 {
		return nil
	}
	out := make([]TimelineEntry, 0, n)
	for i := 0; i < n; i++ {
		var e TimelineEntry
		if i < len(started) {
			e.Started = started[i]
		}
		if i < len(finished) {
			e.Finished = finished[i]
		}
		switch {
		case e.Finished != "":
			e.State = "finished"
		case i == n-1 && (status == "reading" || status == "paused" || status == "dnf"):
			e.State = status
		}
		e.Label = composeTimelineLabel(e)
		out = append(out, e)
	}
	return out
}

// composeTimelineLabel builds the a11y-facing aria-label for a single
// timeline entry, covering the four state buckets plus the fallback.
// Kept plain-text (no punctuation cues like "·") so screen readers
// produce natural prose.
func composeTimelineLabel(e TimelineEntry) string {
	switch e.State {
	case "finished":
		if e.Started != "" {
			return "Started " + e.Started + ", finished " + e.Finished
		}
		return "Finished " + e.Finished
	case "reading":
		return "Reading since " + e.Started
	case "paused":
		return "Paused, last started " + e.Started
	case "dnf":
		return "Stopped reading, last started " + e.Started
	}
	if e.Started != "" && e.Finished == "" {
		return "Started " + e.Started + ", no finish date recorded"
	}
	if e.Started == "" && e.Finished != "" {
		return "Finished " + e.Finished + ", no start date recorded"
	}
	return ""
}

func extractTimelineLines(b *body.Body) []string {
	for _, bl := range b.Blocks {
		if bl.Kind != body.KindTimeline {
			continue
		}
		tp, ok := bl.Parsed.(*body.TimelineParsed)
		if !ok || tp == nil {
			return nil
		}
		out := make([]string, 0, len(tp.Events))
		for _, ev := range tp.Events {
			line := strings.TrimRight(string(ev.Raw), "\r\n")
			line = strings.TrimPrefix(line, "- ")
			if line != "" {
				out = append(out, line)
			}
		}
		return out
	}
	return nil
}

// ImportPageData is the template data for import.html.
type ImportPageData struct {
	PageCommon
}

// ImportPage renders /import.
func (d *Dependencies) ImportPage(w http.ResponseWriter, r *http.Request) {
	d.renderHTML(w, r, "import", ImportPageData{
		PageCommon: d.newPageCommon(r, "import"),
	})
}

// HealthSignature is the stable token emitted by /healthz. The
// single-instance probe in internal/platform/singleton checks for it
// so "something is listening on our port" can be distinguished from
// "another Shelf is listening on our port." Keep the value stable
// across releases.
const HealthSignature = "shelf ok"

// Health is the plaintext liveness probe. The response body is the
// HealthSignature constant, which the single-instance probe looks for.
func (d *Dependencies) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(HealthSignature))
}

// NotFoundHandler is installed as the default route handler for the
// mux. Produces an HTML error page for browser routes and a JSON
// envelope for anything under /api/.
func (d *Dependencies) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		d.writeJSONError(w, r, http.StatusNotFound, "not_found", "no such endpoint")
		return
	}
	d.renderErrorPage(w, r, http.StatusNotFound, "This page does not exist.")
}
