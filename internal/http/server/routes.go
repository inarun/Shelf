package server

import (
	"net/http"

	"github.com/inarun/Shelf/internal/http/handlers"
	"github.com/inarun/Shelf/internal/http/static"
)

// registerRoutes declares the URL surface for the Shelf server. Routes
// use Go 1.22+ ServeMux method+path patterns. Order is irrelevant;
// the most-specific pattern wins.
func registerRoutes(mux *http.ServeMux, h *handlers.Dependencies) {
	// Root redirect.
	mux.HandleFunc("GET /{$}", h.LibraryIndex)

	// SSR pages.
	mux.HandleFunc("GET /library", h.LibraryView)
	mux.HandleFunc("GET /books/{filename}", h.BookDetail)
	mux.HandleFunc("GET /import", h.ImportPage)

	// Static assets.
	mux.Handle("GET /static/", http.StripPrefix("/static/", static.Handler()))

	// Liveness.
	mux.HandleFunc("GET /healthz", h.Health)

	// JSON API.
	mux.HandleFunc("PATCH /api/books/{filename}", h.PatchBook)
	mux.HandleFunc("POST /api/import/plan", h.PlanImport)
	mux.HandleFunc("POST /api/import/apply", h.ApplyImport)

	// Catch-all: routes the server doesn't recognize. The handler
	// distinguishes /api/* (JSON envelope) from browser routes (HTML
	// error page) itself.
	mux.HandleFunc("/", h.NotFoundHandler)
}
