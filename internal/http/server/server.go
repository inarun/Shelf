// Package server assembles the Shelf HTTP stack: Dependencies bundle,
// middleware chain, route registration, and process-start key
// generation. Callers typically instantiate one *Server at startup
// and hand its Handler() to an http.Server.
package server

import (
	"fmt"
	"log/slog"

	"github.com/inarun/Shelf/internal/config"
	"github.com/inarun/Shelf/internal/covers"
	"github.com/inarun/Shelf/internal/http/handlers"
	"github.com/inarun/Shelf/internal/http/middleware"
	"github.com/inarun/Shelf/internal/http/templates"
	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/providers/metadata"
	"github.com/inarun/Shelf/internal/providers/reading/audiobookshelf"
	"github.com/inarun/Shelf/internal/recommender/llm"

	"net/http"
)

// Dependencies holds everything New needs from the bootstrap layer.
// Config is passed rather than just Bind/Port so future middlewares
// can consult other fields without changing this struct's shape.
// Metadata and Covers may be nil; handlers return 503 in that case.
type Dependencies struct {
	Config               *config.Config
	Store                *store.Store
	Syncer               *sync.Syncer
	Metadata             metadata.Provider
	Covers               *covers.Cache
	AudiobookshelfClient *audiobookshelf.Client
	LLMClient            *llm.Client
	RecommenderEnabled   bool
	BooksAbs             string
	BackupsRoot          string
	Logger               *slog.Logger

	// ShutdownSignal, if non-nil, is forwarded to the handler layer
	// and signalled by POST /api/shutdown to trigger a graceful exit
	// from cmd/shelf/main.go. Owned upstream; server just threads it.
	ShutdownSignal chan<- struct{}
}

// Server is the assembled stack. Immutable after New; all state lives
// on the embedded *store.Store, *sync.Syncer, and *slog.Logger which
// are themselves safe for concurrent use.
type Server struct {
	deps    Dependencies
	keys    *Keys
	handler http.Handler
}

// New wires handlers, middleware, and keys. Fails if the templates
// fail to parse or the OS CSPRNG fails.
func New(deps Dependencies) (*Server, error) {
	if deps.Config == nil || deps.Store == nil || deps.Syncer == nil || deps.Logger == nil {
		return nil, fmt.Errorf("server: Dependencies has required nil field")
	}

	tmpl, err := templates.Parse()
	if err != nil {
		return nil, fmt.Errorf("server: parse templates: %w", err)
	}
	keys, err := NewKeys()
	if err != nil {
		return nil, err
	}

	handlerDeps := &handlers.Dependencies{
		Store:                deps.Store,
		Syncer:               deps.Syncer,
		Metadata:             deps.Metadata,
		Covers:               deps.Covers,
		AudiobookshelfClient: deps.AudiobookshelfClient,
		LLMClient:            deps.LLMClient,
		RecommenderEnabled:   deps.RecommenderEnabled,
		BooksAbs:             deps.BooksAbs,
		BackupsRoot:          deps.BackupsRoot,
		Templates:            tmpl,
		HMACKey:              keys.HMAC[:],
		Logger:               deps.Logger,
		ShutdownSignal:       deps.ShutdownSignal,
	}

	mux := http.NewServeMux()
	registerRoutes(mux, handlerDeps)

	chain := middleware.Chain{
		middleware.Recover(deps.Logger),
		middleware.RequestID,
		middleware.Logging(deps.Logger),
		middleware.Host(deps.Config.Server.Bind, deps.Config.Server.Port),
		middleware.SecurityHeaders,
		middleware.Session,
		middleware.CSRF(keys.HMAC[:]),
	}

	return &Server{
		deps:    deps,
		keys:    keys,
		handler: chain.Then(mux),
	}, nil
}

// Handler returns the fully-wrapped http.Handler ready to hand to an
// http.Server.
func (s *Server) Handler() http.Handler { return s.handler }
