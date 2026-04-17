package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitEvent waits for the next event or fails after timeout.
func waitEvent(t *testing.T, w *Watcher, timeout time.Duration) Event {
	t.Helper()
	select {
	case ev := <-w.Events():
		return ev
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for event after %s", timeout)
	}
	return Event{}
}

// drainEvents attempts to drain all pending events, returning them.
func drainEvents(w *Watcher, window time.Duration) []Event {
	var out []Event
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		select {
		case ev := <-w.Events():
			out = append(out, ev)
		case <-time.After(50 * time.Millisecond):
			// no event in this window — keep looping until deadline
		}
	}
	return out
}

func newTestWatcher(t *testing.T, dir string) *Watcher {
	t.Helper()
	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the debounce short so tests don't have to wait 500ms each.
	w.setDebounce(80 * time.Millisecond)
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestNew_StartsAndCloses(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Second close is a no-op.
	if err := w.Close(); err != nil {
		t.Errorf("second close returned: %v", err)
	}
}

func TestEmit_Create(t *testing.T) {
	dir := t.TempDir()
	w := newTestWatcher(t, dir)

	path := filepath.Join(dir, "new.md")
	if err := os.WriteFile(path, []byte("# New\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, w, time.Second)
	if ev.Kind != KindCreate || ev.Path != path {
		t.Errorf("got %+v, want KindCreate for %s", ev, path)
	}
}

func TestEmit_Write_Debounced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.md")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newTestWatcher(t, dir)

	// Rapid writes — the debouncer should coalesce these into a single
	// KindWrite after the window elapses.
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(path, []byte("body"), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ev := waitEvent(t, w, time.Second)
	if ev.Kind != KindWrite {
		t.Errorf("got %+v, want KindWrite", ev)
	}
	more := drainEvents(w, 200*time.Millisecond)
	if len(more) > 0 {
		t.Errorf("extra events emitted: %+v", more)
	}
}

func TestEmit_Remove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doomed.md")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newTestWatcher(t, dir)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, w, time.Second)
	if ev.Kind != KindRemove {
		t.Errorf("got %+v, want KindRemove", ev)
	}
}

func TestEmit_IgnoresTempFiles(t *testing.T) {
	dir := t.TempDir()
	w := newTestWatcher(t, dir)

	tmpPath := filepath.Join(dir, ".book.md.0123456789abcdef.shelf.tmp")
	if err := os.WriteFile(tmpPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ms := drainEvents(w, 200*time.Millisecond)
	if len(ms) > 0 {
		t.Errorf("temp file leaked as event: %+v", ms)
	}
}

func TestEmit_IgnoresNonMd(t *testing.T) {
	dir := t.TempDir()
	w := newTestWatcher(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "cover.png"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ms := drainEvents(w, 200*time.Millisecond)
	if len(ms) > 0 {
		t.Errorf("non-.md file leaked: %+v", ms)
	}
}

func TestEmit_AtomicRename_SettlesAsWrite(t *testing.T) {
	// Simulate the atomic-write pattern: create tmp, rename-over
	// existing target. fsnotify raw events on Windows are typically
	// Create(tmp) + Rename(target)/Create(target); the debouncer
	// should settle to a single KindWrite for the target.
	dir := t.TempDir()
	target := filepath.Join(dir, "note.md")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newTestWatcher(t, dir)

	tmp := filepath.Join(dir, ".note.md.fedcba9876543210.shelf.tmp")
	if err := os.WriteFile(tmp, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, target); err != nil {
		t.Fatal(err)
	}
	ev := waitEvent(t, w, time.Second)
	if ev.Kind != KindWrite || ev.Path != target {
		t.Errorf("atomic rename settled as %+v, want KindWrite for %s", ev, target)
	}
	more := drainEvents(w, 200*time.Millisecond)
	for _, e := range more {
		if filepath.Base(e.Path) != "note.md" {
			t.Errorf("stray event on atomic rename: %+v", e)
		}
	}
}

func TestIsRelevant(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Book by Author.md", true},
		{"Book.MD", true},
		{".hidden.md", false},
		{"cover.png", false},
		{"", false},
		{"no-ext", false},
	}
	for _, c := range cases {
		if got := isRelevant(c.in); got != c.want {
			t.Errorf("isRelevant(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsShelfTempFile_Watcher(t *testing.T) {
	// Mirrors index/sync's test; watcher and sync use independent
	// copies so changing one doesn't silently drift the other.
	cases := []struct {
		in   string
		want bool
	}{
		{".book.md.0123456789abcdef.shelf.tmp", true},
		{".book.md.0123456789ABCDEF.shelf.tmp", false},
		{".book.md.abc.shelf.tmp", false},
		{"book.md.0123456789abcdef.shelf.tmp", false},
		{".book.md.0123456789abcdef.shelf.tmp.md", false},
	}
	for _, c := range cases {
		if got := isShelfTempFile(c.in); got != c.want {
			t.Errorf("isShelfTempFile(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
