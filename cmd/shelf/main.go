// Command shelf is the main entry point for the Shelf local-first
// reading journal. It loads config, initializes a file-only slog
// handler, opens the SQLite index, performs a full scan, starts the
// filesystem watcher, serves the HTTP UI on the configured bind
// address, and runs a Windows system-tray icon. Graceful shutdown
// triggers on SIGINT/SIGTERM, on a fatal HTTP error, or when the
// user clicks "Quit" in the tray menu.
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
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/inarun/Shelf/internal/config"
	"github.com/inarun/Shelf/internal/covers"
	"github.com/inarun/Shelf/internal/http/handlers"
	httpserver "github.com/inarun/Shelf/internal/http/server"
	"github.com/inarun/Shelf/internal/index/store"
	syncpkg "github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/platform/autostart"
	"github.com/inarun/Shelf/internal/platform/browser"
	"github.com/inarun/Shelf/internal/platform/singleton"
	"github.com/inarun/Shelf/internal/providers/metadata"
	"github.com/inarun/Shelf/internal/providers/metadata/openlibrary"
	"github.com/inarun/Shelf/internal/tray"
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

	if err := run(cfg, logger, logPath, *configPath); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// run is the real entry point — separated from main() so defers fire
// and a returned error flows through a single exit path.
func run(cfg *config.Config, logger *slog.Logger, logPath, configFlag string) error {
	libraryURL := fmt.Sprintf("http://127.0.0.1:%d/library", cfg.Server.Port)

	// Single-instance probe: if a live Shelf is already listening on
	// our port, open its library page in the default browser and exit
	// cleanly. The probe only accepts 127.0.0.1 so we never falsely
	// "defer" to an external listener.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	probeErr := singleton.Probe(probeCtx, cfg.Server.Port, handlers.HealthSignature)
	probeCancel()
	if probeErr == nil {
		logger.Info("existing Shelf instance detected; opening browser", "url", libraryURL)
		fmt.Printf("shelf: already running on port %d; opening browser.\n", cfg.Server.Port)
		if err := browser.Open(libraryURL); err != nil {
			logger.Warn("browser.Open failed", "err", err)
			return fmt.Errorf("open browser for existing instance: %w", err)
		}
		return nil
	}

	booksAbs := cfg.BooksAbsolutePath()
	backupsRoot := filepath.Join(cfg.Data.Directory, "backups")
	coversRoot := filepath.Join(cfg.Data.Directory, "covers")
	dbPath := filepath.Join(cfg.Data.Directory, "shelf.db")

	if err := os.MkdirAll(backupsRoot, 0o700); err != nil {
		return fmt.Errorf("create backups dir: %w", err)
	}

	coverCache, err := covers.New(coversRoot)
	if err != nil {
		return fmt.Errorf("init covers cache: %w", err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	sy := syncpkg.New(st, booksAbs)

	// Open Library metadata provider. Gated on providers.openlibrary.enabled
	// so a user who hasn't opted in gets zero outbound HTTP capability — the
	// add-book handlers already return 503 and the /add page shows a
	// "provider not configured" banner when Metadata is nil. Every outbound
	// request (when enabled) enforces its own timeout + size cap + host
	// allowlist — see internal/providers/metadata/openlibrary.
	var olClient metadata.Provider
	if cfg.Providers.OpenLibrary.Enabled {
		olClient = openlibrary.New()
	} else {
		logger.Info("openlibrary provider disabled via config; add-book flow unavailable")
	}

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
		Metadata:    olClient,
		Covers:      coverCache,
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
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info("shelf listening",
		"addr", httpSrv.Addr,
		"books", booksAbs,
		"logs", logPath,
	)
	fmt.Printf("shelf: listening on http://%s  (logs: %s)\n", httpSrv.Addr, logPath)

	// HTTP server on its own goroutine; a fatal listen error cancels
	// the root context so tray + bootstrap unwind.
	httpErrCh := make(chan error, 1)
	go func() {
		err := httpSrv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- nil
			return
		}
		httpErrCh <- err
	}()

	autoH := newAutostartHandle(logger, configFlag)

	trayCfg := tray.Config{
		Tooltip: fmt.Sprintf("Shelf — http://127.0.0.1:%d", cfg.Server.Port),
		OnOpen: func() {
			if err := browser.Open(libraryURL); err != nil {
				logger.Warn("browser.Open from tray failed", "err", err)
			}
		},
		IsAutostartEnabled: func() bool {
			if autoH == nil {
				return false
			}
			enabled, _, err := autoH.Status()
			if err != nil {
				logger.Warn("autostart status", "err", err)
				return false
			}
			return enabled
		},
		OnToggleAutostart: func(target bool) error {
			if autoH == nil {
				return nil
			}
			var err error
			if target {
				err = autoH.Enable()
			} else {
				err = autoH.Disable()
			}
			if err != nil {
				logger.Warn("autostart toggle", "target", target, "err", err)
				return err
			}
			logger.Info("autostart toggled", "enabled", target)
			return nil
		},
		OnQuit: func() {
			logger.Info("tray quit requested")
			cancel()
		},
	}

	// Fire and forget: tray exits fast on non-Windows (ErrNotSupported)
	// so we must not block shutdown on its error channel. Real tray
	// quits cancel the root context via OnQuit.
	go func() {
		if err := tray.Run(trayCfg); err != nil && !errors.Is(err, tray.ErrNotSupported) {
			logger.Warn("tray exited", "err", err)
		}
	}()

	// Auto-open the library URL shortly after startup on Windows so
	// double-clicking shelf.exe behaves like an app launch. Non-Windows
	// dev runs stay headless for sanity.
	if runtime.GOOS == "windows" {
		go func() {
			time.Sleep(250 * time.Millisecond)
			if err := browser.Open(libraryURL); err != nil {
				logger.Warn("auto-open browser failed", "err", err)
			}
		}()
	}

	// Wait for one of:
	//   * OS shutdown signal
	//   * HTTP server exit (graceful or fatal)
	//   * Context cancellation (tray Quit)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var runErr error
	select {
	case <-sigCh:
		logger.Info("os signal received")
	case err := <-httpErrCh:
		if err != nil {
			logger.Error("http server", "err", err)
			runErr = err
		} else {
			logger.Info("http server exited")
		}
	case <-ctx.Done():
		logger.Info("root context cancelled")
	}

	// Initiate graceful shutdown regardless of which branch fired.
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Warn("http shutdown", "err", err)
	}

	tray.Stop()

	// Drain channels so the HTTP goroutine gets to exit before we return.
	select {
	case <-httpErrCh:
	case <-time.After(3 * time.Second):
		logger.Warn("http goroutine did not exit within 3s")
	}

	logger.Info("shelf stopped")
	return runErr
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

// newAutostartHandle builds the autostart registration handle. The
// command string is the current binary's absolute path, quoted, plus
// the --config flag if the user launched with one so an autostart
// entry keeps pointing at the same config the user picked.
//
// Returns nil (not an error) if os.Executable fails, so the tray can
// still run with autostart toggling silently disabled. A failed
// Executable() lookup on Windows is exceedingly rare.
func newAutostartHandle(logger *slog.Logger, configFlag string) *autostart.Autostart {
	exePath, err := os.Executable()
	if err != nil {
		logger.Warn("autostart: os.Executable failed — autostart unavailable", "err", err)
		return nil
	}
	absExe, err := filepath.Abs(exePath)
	if err == nil {
		exePath = absExe
	}
	command := fmt.Sprintf(`"%s"`, exePath)
	if configFlag != "" {
		absCfg, err := filepath.Abs(configFlag)
		if err == nil {
			command = fmt.Sprintf(`"%s" --config "%s"`, exePath, absCfg)
		}
	}
	handle, err := autostart.New(autostart.AppName, command)
	if err != nil {
		logger.Warn("autostart: new handle failed", "err", err)
		return nil
	}
	return handle
}
