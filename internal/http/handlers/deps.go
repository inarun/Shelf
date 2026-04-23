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
	"github.com/inarun/Shelf/internal/recommender/llm"
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
	LLMClient            *llm.Client
	RecommenderEnabled   bool
	BooksAbs             string
	BackupsRoot          string
	Templates            *template.Template
	HMACKey              []byte
	Logger               *slog.Logger

	// ShutdownSignal is closed/signalled by the web-UI shutdown
	// handler (POST /api/shutdown) to request a clean process exit.
	// Owned by cmd/shelf/main.go's select loop; handlers only send.
	// Buffered size 1 + select-default in the handler makes duplicate
	// requests idempotent.
	ShutdownSignal chan<- struct{}
}
