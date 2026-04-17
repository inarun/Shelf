// Package watcher is an fsnotify wrapper that emits typed, coalesced
// events for .md file changes in the books directory.
//
// Raw fsnotify events are noisy: editors and OS-level atomic-rename
// flows typically emit several events per logical save, and Windows in
// particular often turns a rename into Remove+Create. The watcher
// coalesces all raw events for a given path into a single typed event
// using a 500ms per-path debouncer, then classifies the final event
// based on the current disk state rather than the raw event type. This
// produces a clean stream of Create/Write/Remove events that the
// indexer can apply idempotently.
//
// Shelf's own atomic-write temp files (.{base}.{16 hex}.shelf.tmp) are
// filtered out so a write by the app doesn't re-fire a watcher event
// for itself.
package watcher
