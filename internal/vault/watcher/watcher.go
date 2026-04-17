package watcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Kind is the typed category of a coalesced event. Zero value is
// unused; valid values are KindCreate, KindWrite, KindRemove.
type Kind int

const (
	// KindCreate signals the path is present and was not previously
	// in the watcher's known set.
	KindCreate Kind = iota + 1
	// KindWrite signals the path is present and was already tracked.
	KindWrite
	// KindRemove signals the path is absent after having been tracked.
	KindRemove
)

// Event is a single coalesced filesystem change.
type Event struct {
	Kind Kind
	Path string
}

// DefaultDebounce is the coalescing window. Editors and OS atomic-
// rename flows typically emit several raw events per logical save over
// ~100-400ms on Windows; 500ms gives the system time to settle before
// the watcher classifies and emits.
const DefaultDebounce = 500 * time.Millisecond

// Watcher wraps a fsnotify.Watcher and presents a debounced, classified
// event stream. One Watcher per books directory. Safe to close multiple
// times; subsequent Close calls are no-ops.
type Watcher struct {
	booksDir string
	debounce time.Duration

	fsw *fsnotify.Watcher

	events chan Event
	errs   chan error
	done   chan struct{}

	mu       sync.Mutex
	pending  map[string]*time.Timer
	known    map[string]bool
	closed   bool
}

// New creates a Watcher bound to booksDir and starts the background
// goroutine. The caller must Close the Watcher when done. The initial
// "known" set is populated from the directory listing so the first
// event after startup classifies correctly.
func New(booksDir string) (*Watcher, error) {
	abs, err := filepath.Abs(booksDir)
	if err != nil {
		return nil, fmt.Errorf("watcher: abs %s: %w", booksDir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("watcher: stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("watcher: %s is not a directory", abs)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watcher: fsnotify: %w", err)
	}
	if err := fsw.Add(abs); err != nil {
		_ = fsw.Close()
		return nil, fmt.Errorf("watcher: add %s: %w", abs, err)
	}

	w := &Watcher{
		booksDir: abs,
		debounce: DefaultDebounce,
		fsw:      fsw,
		events:   make(chan Event, 128),
		errs:     make(chan error, 8),
		done:     make(chan struct{}),
		pending:  map[string]*time.Timer{},
		known:    map[string]bool{},
	}
	if err := w.seedKnown(); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

// Events returns the channel of coalesced events. Close closes it.
func (w *Watcher) Events() <-chan Event { return w.events }

// Errors returns non-fatal errors from the underlying fsnotify watcher.
func (w *Watcher) Errors() <-chan error { return w.errs }

// Close stops the watcher and waits for the background goroutine to
// exit. All outstanding debounce timers are stopped; pending events
// that hadn't fired are dropped.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	for _, t := range w.pending {
		t.Stop()
	}
	w.pending = nil
	w.mu.Unlock()

	err := w.fsw.Close()
	<-w.done
	close(w.events)
	close(w.errs)
	return err
}

// WithDebounce is a test-only knob to shorten the coalescing window.
// Unexported setter to keep production callers on the constant.
func (w *Watcher) setDebounce(d time.Duration) {
	w.debounce = d
}

func (w *Watcher) seedKnown() error {
	entries, err := os.ReadDir(w.booksDir)
	if err != nil {
		return fmt.Errorf("watcher: list %s: %w", w.booksDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if isRelevant(e.Name()) {
			w.known[e.Name()] = true
		}
	}
	return nil
}

func (w *Watcher) loop() {
	defer close(w.done)
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.schedule(ev)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			select {
			case w.errs <- err:
			default:
				// Drop if the error channel is full; the underlying
				// watcher is still functional and the user is already
				// seeing problems.
			}
		}
	}
}

// schedule either starts or resets a debounce timer for the event's
// path. When the timer fires, emit() classifies based on current disk
// state.
func (w *Watcher) schedule(raw fsnotify.Event) {
	// Ignore chmod-only events; they don't affect indexed content.
	if raw.Op == fsnotify.Chmod {
		return
	}
	base := filepath.Base(raw.Name)
	if isShelfTempFile(base) {
		return
	}
	if !isRelevant(base) {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if t, ok := w.pending[raw.Name]; ok {
		t.Reset(w.debounce)
		return
	}
	w.pending[raw.Name] = time.AfterFunc(w.debounce, func() { w.emit(raw.Name) })
}

// emit inspects disk at the given path and classifies the event.
// Atomic renames that present as Remove+Create settle into a single
// KindCreate or KindWrite here, depending on whether the target was
// already in the known set.
func (w *Watcher) emit(path string) {
	base := filepath.Base(path)
	_, statErr := os.Stat(path)

	var kind Kind
	var shouldEmit bool

	w.mu.Lock()
	delete(w.pending, path)
	if w.closed {
		w.mu.Unlock()
		return
	}
	wasKnown := w.known[base]
	switch {
	case !errors.Is(statErr, os.ErrNotExist) && statErr == nil:
		// File exists.
		if wasKnown {
			kind = KindWrite
		} else {
			kind = KindCreate
			w.known[base] = true
		}
		shouldEmit = true
	case errors.Is(statErr, os.ErrNotExist):
		if wasKnown {
			kind = KindRemove
			delete(w.known, base)
			shouldEmit = true
		}
	default:
		// Some other stat error — surface on Errors.
		select {
		case w.errs <- fmt.Errorf("watcher: stat %s: %w", path, statErr):
		default:
		}
	}
	w.mu.Unlock()

	if !shouldEmit {
		return
	}
	select {
	case w.events <- Event{Kind: kind, Path: path}:
	case <-time.After(time.Second):
		// Consumer blocked for a full second — drop the event rather
		// than letting the watcher lock up. This is better than
		// blocking indefinitely; a consumer that falls behind will
		// eventually catch up on the next event it misses nothing
		// durable because the index is re-derivable from a full scan.
	}
}
