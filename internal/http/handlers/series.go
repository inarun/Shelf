package handlers

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/inarun/Shelf/internal/index/store"
)

// SeriesListData is the template data for series_list.html.
type SeriesListData struct {
	PageCommon
	Series []store.SeriesSummary
}

// SeriesList renders /series.
func (d *Dependencies) SeriesList(w http.ResponseWriter, r *http.Request) {
	series, err := d.Store.ListSeries(r.Context())
	if err != nil {
		d.Logger.Error("list series", "err", err)
		d.renderErrorPage(w, r, http.StatusInternalServerError, "Could not load series.")
		return
	}
	d.renderHTML(w, r, "series_list", SeriesListData{
		PageCommon: d.newPageCommon(r, "series"),
		Series:     series,
	})
}

// SeriesDetailData is the template data for series_detail.html.
type SeriesDetailData struct {
	PageCommon
	Series store.SeriesDetail
}

// SeriesDetail renders /series/{name}. The name path value is
// URL-unescaped; case-insensitive match via the store. 404 if no series
// has that name (or if it has zero books).
func (d *Dependencies) SeriesDetail(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("name")
	if raw == "" {
		d.renderErrorPage(w, r, http.StatusBadRequest, "Missing series name.")
		return
	}
	name, err := url.PathUnescape(raw)
	if err != nil {
		d.renderErrorPage(w, r, http.StatusBadRequest, "Invalid series name.")
		return
	}
	detail, err := d.Store.GetSeriesByName(r.Context(), name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			d.renderErrorPage(w, r, http.StatusNotFound,
				"No series named "+name+" in the index.")
			return
		}
		d.Logger.Error("get series", "name", name, "err", err)
		d.renderErrorPage(w, r, http.StatusInternalServerError, "Could not load series.")
		return
	}
	d.renderHTML(w, r, "series_detail", SeriesDetailData{
		PageCommon: d.newPageCommon(r, "series"),
		Series:     *detail,
	})
}
