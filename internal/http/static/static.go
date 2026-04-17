// Package static embeds the vanilla HTML/CSS/JS frontend assets into
// the binary and exposes them as a stdlib http.FileServer. No external
// resources, no build step, no bundler — SKILL.md §Tech stack.
package static

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed app.css app.js favicon.svg manifest.webmanifest sw.js
var assets embed.FS

// FS returns the embedded asset filesystem rooted at the asset files
// themselves (no "static" prefix). Mount it under a /static/ URL prefix
// at the handler layer via http.StripPrefix.
func FS() fs.FS { return assets }

// Handler returns an http.Handler that serves the embedded assets with
// cache-friendly headers. Mount via http.StripPrefix("/static/", ...).
// Assets are treated as effectively immutable across a process lifetime;
// the HMAC key regenerates on restart so long caching is safe.
func Handler() http.Handler {
	fileServer := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fileServer.ServeHTTP(w, r)
	})
}

// ManifestHandler serves the PWA manifest at origin root with the
// standard application/manifest+json MIME type. The manifest must be
// declared in <link rel="manifest"> from HTML at origin root to be
// picked up by install prompts.
func ManifestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := assets.ReadFile("manifest.webmanifest")
		if err != nil {
			http.Error(w, "manifest not available", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(data)
	})
}

// ServiceWorkerHandler serves the service worker at origin root. Scope
// defaults to the directory the file is served from; serving at "/sw.js"
// gives the worker scope over the whole origin, which is required for
// the library/book-detail/import shells to be cacheable offline.
//
// Service-Worker-Allowed is not needed here because the scope matches
// the serving path's directory. Cache-Control is short so a new binary
// can ship a new worker quickly on next check.
func ServiceWorkerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := assets.ReadFile("sw.js")
		if err != nil {
			http.Error(w, "service worker not available", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Service-Worker-Allowed", "/")
		_, _ = w.Write(data)
	})
}
