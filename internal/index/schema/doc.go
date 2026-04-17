// Package schema owns the SQLite index schema and the migration runner
// that brings a database up to the current version.
//
// Migrations live as numbered .sql files under migrations/, embedded
// into the binary via go:embed. Migrate applies any file whose version
// (the leading integer in the filename, e.g. 001) is not yet present in
// the schema_migrations bookkeeping table. Each migration runs in its
// own transaction so a failure mid-migration rolls back cleanly.
//
// The schema is vault-is-truth friendly: every row is reconstructible
// from the vault. Nothing is stored here that isn't derivable from a
// .md file's frontmatter + filename + stat, so dropping the DB and
// running a full scan must reproduce an equivalent state.
package schema
