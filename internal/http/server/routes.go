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
	mux.HandleFunc("GET /sync", h.SyncPage)
	mux.HandleFunc("GET /migrate", h.MigratePage)
	mux.HandleFunc("GET /add", h.AddPage)
	mux.HandleFunc("GET /series", h.SeriesList)
	mux.HandleFunc("GET /series/{name}", h.SeriesDetail)
	mux.HandleFunc("GET /stats", h.Stats)

	// Static assets.
	mux.Handle("GET /static/", http.StripPrefix("/static/", static.Handler()))

	// PWA surface. Manifest + service worker are served at origin root
	// so the service worker scope covers /library, /books/*, /import.
	mux.Handle("GET /manifest.webmanifest", static.ManifestHandler())
	mux.Handle("GET /sw.js", static.ServiceWorkerHandler())

	// Cover images — served from the on-disk cache under
	// {dataDir}/covers/. Content-addressed filenames mean the handler
	// can use aggressive immutable cache headers.
	mux.HandleFunc("GET /covers/{filename}", h.ServeCover)

	// Liveness.
	mux.HandleFunc("GET /healthz", h.Health)

	// JSON API.
	mux.HandleFunc("PATCH /api/books/{filename}", h.PatchBook)
	mux.HandleFunc("POST /api/books/{filename}/cover", h.RefreshCover)
	mux.HandleFunc("POST /api/import/plan", h.PlanImport)
	mux.HandleFunc("POST /api/import/apply", h.ApplyImport)
	mux.HandleFunc("POST /api/sync/audiobookshelf/plan", h.PlanSyncAudiobookshelf)
	mux.HandleFunc("POST /api/sync/audiobookshelf/apply", h.ApplySyncAudiobookshelf)
	mux.HandleFunc("POST /api/migrate/plan", h.PlanMigrate)
	mux.HandleFunc("POST /api/migrate/apply", h.ApplyMigrate)
	mux.HandleFunc("POST /api/add/lookup", h.AddLookup)
	mux.HandleFunc("POST /api/add/search", h.AddSearch)
	mux.HandleFunc("POST /api/add/cover", h.AddCover)
	mux.HandleFunc("POST /api/add/create", h.AddCreate)
	mux.HandleFunc("GET /api/recommendations/profile", h.GetRecommendationsProfile)

	// Catch-all: routes the server doesn't recognize. The handler
	// distinguishes /api/* (JSON envelope) from browser routes (HTML
	// error page) itself.
	mux.HandleFunc("/", h.NotFoundHandler)
}
