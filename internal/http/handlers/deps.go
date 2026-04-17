// Package handlers holds the Shelf HTTP route handlers: SSR pages for
// library/book/import views and the small JSON API surface that the
// frontend JS calls (PATCH book, plan/apply import). Shared data types,
// validators, and the Dependencies bundle live here; middleware lives
// in internal/http/middleware and server wiring in internal/http/server.
package handlers

import (
	"html/template"
	"log/slog"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/index/sync"
)

// Dependencies bundles the collaborators every handler needs. Wired
// once by the server at startup and passed by value (it's pointer-heavy
// internally) to each handler constructor.
type Dependencies struct {
	Store       *store.Store
	Syncer      *sync.Syncer
	BooksAbs    string
	BackupsRoot string
	DataDir     string
	Templates   *template.Template
	HMACKey     []byte
	Logger      *slog.Logger
}
