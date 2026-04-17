package handlers

import (
	"errors"
	"net/http"
	"os"

	"github.com/inarun/Shelf/internal/covers"
)

// ServeCover serves a cached cover image from {dataDir}/covers/. Both
// the URL path value and the on-disk path go through the covers package's
// own validators (sha256+ext filename pattern + ValidateWithinRoot), so
// no path-traversal or arbitrary-file-read bug is possible through this
// handler.
func (d *Dependencies) ServeCover(w http.ResponseWriter, r *http.Request) {
	if d.Covers == nil {
		d.writeJSONError(w, r, http.StatusServiceUnavailable, "server", "covers not configured")
		return
	}
	name := r.PathValue("filename")
	abs, err := d.Covers.AbsPath(name)
	if err != nil {
		if errors.Is(err, covers.ErrInvalidFilename) {
			http.NotFound(w, r)
			return
		}
		d.Logger.Warn("cover resolve",
			"name", name,
			"err", err,
		)
		http.NotFound(w, r)
		return
	}
	// #nosec G304,G703 -- abs has passed Covers.AbsPath, which enforces
	// internal/vault/paths.ValidateWithinRoot plus the sha256+ext filename
	// allowlist (64 hex chars + ".jpg"/".png"). Anything user-controlled
	// from the URL path value has been compared against that regex and
	// resolved against the validated covers root before this Open.
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		d.Logger.Warn("cover open",
			"name", name,
			"err", err,
		)
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", covers.ContentTypeFor(name))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, info.ModTime(), f)
}
