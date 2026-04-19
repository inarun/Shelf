// Package migrate handles one-shot transformations of the on-disk vault
// shape. v0.2.1 Session 16 introduces the rating-shape migration:
// rewriting legacy scalar `rating: N` frontmatter entries into the
// canonical `rating: {trial_system: {}, overall: N}` map shape
// introduced in Session 15. The frontmatter parser accepts both shapes,
// so the migration is purely syntactic — it doesn't change the runtime
// meaning of any rating, it just normalizes on-disk YAML so future edits
// don't leave notes in a mixed state.
//
// Shape mirrors internal/vault/rename: a pure BuildPlan produces a
// JSON-serializable Plan with WillMigrate / WillSkip / Conflicts
// buckets, and Apply executes a user-confirmed Plan — pre-apply backup,
// per-note re-read + staleness pair check, atomic write, reindex. The
// shared backup and atomic primitives live in internal/vault/backup and
// internal/vault/atomic.
//
// Future migrations (e.g., rating vocabulary rename post-v0.3) can ride
// the same Plan/Apply shape; add a new EntryKind or a sibling package.
package migrate
