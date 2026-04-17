// Package static embeds the vanilla HTML/CSS/JS frontend assets into
// the binary and exposes them as a stdlib http.FileServer. No external
// resources, no build step, no bundler — SKILL.md §Tech stack.
package static

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed app.css app.js favicon.svg
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
