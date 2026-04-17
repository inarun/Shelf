package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/inarun/Shelf/internal/http/middleware"
)

// APIError is the uniform JSON error envelope. The string codes match
// the small set documented in the Session 4 plan:
// invalid / not_found / stale / csrf / server / host.
type APIError struct {
	Error APIErrorBody `json:"error"`
}

// APIErrorBody is the inner payload.
type APIErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// writeJSONError emits the error envelope. Safe to call before or
// after status is committed; writes a single JSON object and sets
// Content-Type.
func (d *Dependencies) writeJSONError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIError{Error: APIErrorBody{
		Code:      code,
		Message:   msg,
		RequestID: middleware.RequestIDFrom(r.Context()),
	}})
}

// renderErrorPage renders the error.html template for browser routes.
// Falls back to a plain-text body if the template itself can't execute.
type errorPageData struct {
	PageCommon
	Status  int
	Message string
}

func (d *Dependencies) renderErrorPage(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	data := errorPageData{
		PageCommon: d.newPageCommon(r, ""),
		Status:     status,
		Message:    msg,
	}
	if err := d.Templates.ExecuteTemplate(w, "error", data); err != nil {
		d.Logger.Error("error-page template failed", "err", err)
		_, _ = w.Write([]byte("<!doctype html><title>" + http.StatusText(status) + "</title><h1>" + http.StatusText(status) + "</h1>"))
	}
}
