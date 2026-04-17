package handlers

import (
	"net/http"

	"github.com/inarun/Shelf/internal/index/store"
)

// StatsData is the template data for stats.html. MaxYearBooks and
// MaxYearPages are precomputed so the template can render bars
// proportional to the largest row without needing per-row arithmetic.
type StatsData struct {
	PageCommon
	Summary       store.StatsSummary
	Years         []store.YearStats
	MaxYearBooks  int64
	MaxYearPages  int64
	OrderedStatus []string
}

// Stats renders /stats.
func (d *Dependencies) Stats(w http.ResponseWriter, r *http.Request) {
	summary, err := d.Store.Stats(r.Context())
	if err != nil {
		d.Logger.Error("stats summary", "err", err)
		d.renderErrorPage(w, r, http.StatusInternalServerError, "Could not load stats.")
		return
	}
	years, err := d.Store.BooksPerYear(r.Context())
	if err != nil {
		d.Logger.Error("stats years", "err", err)
		d.renderErrorPage(w, r, http.StatusInternalServerError, "Could not load year stats.")
		return
	}

	var maxB, maxP int64
	for _, y := range years {
		if y.Books > maxB {
			maxB = y.Books
		}
		if y.Pages > maxP {
			maxP = y.Pages
		}
	}

	d.renderHTML(w, r, "stats", StatsData{
		PageCommon:    d.newPageCommon(r, "stats"),
		Summary:       *summary,
		Years:         years,
		MaxYearBooks:  maxB,
		MaxYearPages:  maxP,
		OrderedStatus: []string{"reading", "paused", "finished", "unread", "dnf"},
	})
}
