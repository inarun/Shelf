// Command shelf is the main entry point for the Shelf local-first
// reading journal. It loads config, initializes a file-only slog
// handler, opens the SQLite index, performs a full scan, starts the
// filesystem watcher, and serves the HTTP UI on the configured bind
// address with graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/inarun/Shelf/internal/config"
	httpserver "github.com/inarun/Shelf/internal/http/server"
	"github.com/inarun/Shelf/internal/index/store"
	syncpkg "github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/watcher"
)

func main() {
	configPath := flag.String("config", "", "path to shelf.toml (default: <binary_dir>/shelf.toml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, config.ErrNoConfig) {
			fmt.Fprintf(os.Stderr,
				"shelf: no config file found. Create shelf.toml next to the binary, "+
					"or point to one with --config <path>.\n  %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "shelf: %v\n", err)
		}
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "shelf: %v\n", err)
		os.Exit(1)
	}

	logger, logPath, err := initLogger(cfg.Data.Directory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shelf: init logger: %v\n", err)
		os.Exit(1)
	}

	if cfg.IsExternalBind() {
		logger.Warn("non-loopback bind — localhost-only is the invariant",
			"bind", cfg.Server.Bind,
			"spec", "SKILL.md §Core Invariants #4",
		)
	}

	if err := run(cfg, logger, logPath); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run is the real entry point — separated from main() so defers fire
// and a returned error flows through a single exit path.
func run(cfg *config.Config, logger *slog.Logger, logPath string) error {
	booksAbs := cfg.BooksAbsolutePath()
	backupsRoot := filepath.Join(cfg.Data.Directory, "backups")
	dbPath := filepath.Join(cfg.Data.Directory, "shelf.db")

	if err := os.MkdirAll(backupsRoot, 0o700); err != nil {
		return fmt.Errorf("create backups dir: %w", err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	sy := syncpkg.New(st, booksAbs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rep, err := sy.FullScan(ctx)
	if err != nil {
		return fmt.Errorf("initial full scan: %w", err)
	}
	logger.Info("initial sync",
		"scanned", rep.Scanned,
		"indexed", rep.Indexed,
		"skipped", rep.Skipped,
		"deleted", rep.Deleted,
		"errors", len(rep.Errors),
	)

	w, err := watcher.New(booksAbs)
	if err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	defer func() { _ = w.Close() }()

	go drainWatcher(ctx, w, sy, logger)

	srv, err := httpserver.New(httpserver.Dependencies{
		Config:      cfg,
		Store:       st,
		Syncer:      sy,
		BooksAbs:    booksAbs,
		BackupsRoot: backupsRoot,
		DataDir:     cfg.Data.Directory,
		Logger:      logger,
	})
	if err != nil {
		return fmt.Errorf("build server: %w", err)
	}

	httpSrv := &http.Server{
		Addr:              net.JoinHostPort(cfg.Server.Bind, strconv.Itoa(cfg.Server.Port)),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownErrCh := make(chan error, 1)
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		logger.Info("shutdown signal received")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		shutdownErrCh <- httpSrv.Shutdown(shutdownCtx)
		cancel()
	}()

	logger.Info("shelf listening",
		"addr", httpSrv.Addr,
		"books", booksAbs,
		"logs", logPath,
	)
	fmt.Printf("shelf: listening on http://%s  (logs: %s)\n", httpSrv.Addr, logPath)

	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen and serve: %w", err)
	}
	// Propagate any shutdown-time error.
	if sErr := <-shutdownErrCh; sErr != nil {
		return fmt.Errorf("shutdown: %w", sErr)
	}
	logger.Info("shelf stopped")
	return nil
}

// initLogger opens {dataDir}/logs/shelf.log for append-only JSON slog.
// Returns (logger, log path, error). Falls back to stderr only if the
// log directory cannot be created — which, given the config-validator
// already guaranteed data.directory is writable, should never happen.
func initLogger(dataDir string) (*slog.Logger, string, error) {
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create logs dir: %w", err)
	}
	logPath := filepath.Join(logDir, "shelf.log")
	// #nosec G304 -- logPath is computed from the validated data.directory.
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("open log file %s: %w", logPath, err)
	}
	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	// Silence the stdlib `log` package's own default output — it may
	// otherwise bleed into stderr when third-party code uses it.
	log.SetOutput(f)
	log.SetPrefix("")
	log.SetFlags(0)
	return logger, logPath, nil
}

// drainWatcher pulls events off the fsnotify-backed watcher channel
// and feeds them to the syncer. Exits when the watcher closes its
// channel or the bootstrap context cancels.
func drainWatcher(ctx context.Context, w *watcher.Watcher, sy *syncpkg.Syncer, logger *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.Errors():
			if !ok {
				return
			}
			logger.Warn("watcher error", "err", err)
		case ev, ok := <-w.Events():
			if !ok {
				return
			}
			kind := mapWatcherKind(ev.Kind)
			if err := sy.Apply(ctx, syncpkg.Event{Kind: kind, Path: ev.Path}); err != nil {
				logger.Warn("sync apply from watcher",
					"path", ev.Path,
					"err", err,
				)
			}
		}
	}
}

func mapWatcherKind(k watcher.Kind) syncpkg.EventKind {
	switch k {
	case watcher.KindCreate:
		return syncpkg.EventCreate
	case watcher.KindWrite:
		return syncpkg.EventWrite
	case watcher.KindRemove:
		return syncpkg.EventRemove
	default:
		return syncpkg.EventWrite
	}
}
