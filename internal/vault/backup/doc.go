// Package backup produces a timestamped, recursive snapshot of the Books
// folder under {data.directory}/backups/books-{UTCstamp}/ before a bulk
// write. It is the pre-bulk-write rollback mechanism required by
// SKILL.md §Core invariant #7 and called by the Goodreads importer's
// and the rename pipeline's Apply paths.
//
// Every regular file under the Books folder is copied — not just the
// ".md" notes the importer would modify. That's the conservative read
// of "snapshot the entire Books folder": if anything in that folder
// turns out to be unexpectedly important, the backup contains it.
//
// Each copy is written via internal/vault/atomic.Write inside the
// timestamped backup directory, so an interrupted snapshot leaves the
// partial backup intact (for operator inspection) but never
// half-written files. The backup directory is validated to live under
// the configured backups root, never inside the vault itself.
package backup
