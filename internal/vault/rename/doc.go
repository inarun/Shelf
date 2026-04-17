// Package rename migrates non-canonical book filenames to the canonical
// "{Title} by {Author}.md" pattern. The pipeline shape mirrors the
// Goodreads importer's: BuildPlan produces a dry-run Plan, Apply
// executes it after a pre-write backup.
//
// Trigger: index rows with CanonicalName == false (Session 2's
// non-canonical flag). The rename target is derived from the note's
// frontmatter title + authors[0] via internal/vault/paths.Generate.
// Notes missing either field produce a conflict, not a silent rename.
//
// Apply uses internal/vault/atomic.Rename for the same-volume move
// (Windows retry loop applies) and drives the index via
// internal/index/sync.Apply with an EventRename, so the new filename
// replaces the old one in the store atomically with the filesystem.
package rename
