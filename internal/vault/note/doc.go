// Package note is the high-level read/write layer for a book note. It
// composes internal/vault/frontmatter and internal/vault/body into a
// typed record and enforces the concurrent-edit guard from
// SKILL.md §Concurrent edit handling.
//
// Two save paths exist. SaveBody writes the full file (frontmatter +
// body) only after re-confirming the on-disk (size, mtime_ns) pair
// matches what was stamped at read time; any mismatch returns ErrStale
// so the UI can prompt the user to reload. SaveFrontmatter is
// unconditional per spec — it reads the file fresh from disk, replays
// only the frontmatter keys the app has mutated on top of that fresh
// copy, and writes atomically. This guarantees that concurrent Obsidian
// edits to untouched frontmatter fields or the body survive a rating or
// status change.
package note
