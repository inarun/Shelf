// Package handlers holds the Shelf HTTP route handlers: SSR pages for
// library/book/import views and the small JSON API surface that the
// frontend JS calls (PATCH book, plan/apply import). Shared data types,
// validators, and the Dependencies bundle live here; middleware lives
// in internal/http/middleware and server wiring in internal/http/server.
package handlers

import (
	"html/template"
	"log/slog"

	"github.com/inarun/Shelf/internal/covers"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/providers/metadata"
	"github.com/inarun/Shelf/internal/providers/reading/audiobookshelf"
)

// Dependencies bundles the collaborators every handler needs. Wired
// once by the server at startup and passed by value (it's pointer-heavy
// internally) to each handler constructor.
//
// Metadata and Covers were added in Session 6 for the add-book flow and
// cover caching respectively. Both are optional in principle (handlers
// check for nil before dereferencing); in practice cmd/shelf always
// wires them.
type Dependencies struct {
	Store                *store.Store
	Syncer               *sync.Syncer
	Metadata             metadata.Provider
	Covers               *covers.Cache
	AudiobookshelfClient *audiobookshelf.Client
	RecommenderEnabled   bool
	BooksAbs             string
	BackupsRoot          string
	DataDir              string
	Templates            *template.Template
	HMACKey              []byte
	Logger               *slog.Logger
}
