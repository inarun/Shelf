package handlers

import (
	"bytes"
	"net/http"

	"github.com/inarun/Shelf/internal/http/middleware"
)

// PageCommon is embedded in every page template data struct so
// renderHTML can inject CSRFToken + RequestID + ActiveNav uniformly.
type PageCommon struct {
	CSRFToken string
	RequestID string
	ActiveNav string
}

// renderHTML executes the named template against data. Data is expected
// to be either a struct embedding PageCommon or a map containing the
// same keys; renderHTML trusts the caller to supply the right shape
// for the target template. Rendering errors are logged and translate
// to a 500 so a broken template never silently ships half-a-page.
//
// Writes to a buffer first so template errors don't leave us sending a
// partial body under a 200 status.
func (d *Dependencies) renderHTML(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buf bytes.Buffer
	if err := d.Templates.ExecuteTemplate(&buf, name, data); err != nil {
		d.Logger.Error("template render",
			"template", name,
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", err,
		)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		if _, werr := w.Write([]byte("<!doctype html><title>500</title><h1>500 Internal Server Error</h1>")); werr != nil {
			d.Logger.Debug("render 500 write failed", "err", werr)
		}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, werr := w.Write(buf.Bytes()); werr != nil {
		d.Logger.Debug("render body write failed",
			"template", name,
			"request_id", middleware.RequestIDFrom(r.Context()),
			"err", werr,
		)
	}
}

// newPageCommon populates CSRFToken/RequestID/ActiveNav from the
// request context and the configured HMAC key. Handlers call this
// when composing the template data struct.
func (d *Dependencies) newPageCommon(r *http.Request, activeNav string) PageCommon {
	return PageCommon{
		CSRFToken: middleware.CSRFTokenFor(r.Context(), d.HMACKey),
		RequestID: middleware.RequestIDFrom(r.Context()),
		ActiveNav: activeNav,
	}
}
