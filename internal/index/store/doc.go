// Package store is the typed CRUD layer over the SQLite index. It hides
// SQL from callers and does all work in single transactions per logical
// operation. Foreign keys are on, journal mode is WAL, and ON DELETE
// CASCADE on book_authors/book_categories means removing a book rips
// out its join rows automatically.
//
// The schema is normalized (authors, categories, series are separate
// tables) because the full scan re-upserts every changed book, and
// diffing normalized rows is deterministic. Joining back for a single
// book read uses N+1 queries; fine for ~150-row libraries and easier
// to follow than a GROUP_CONCAT one-shot.
package store
