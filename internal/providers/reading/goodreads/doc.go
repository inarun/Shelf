// Package goodreads implements the Goodreads CSV importer. The full
// flow, per SKILL.md §Goodreads CSV import, is:
//
//  1. Parse a Goodreads export CSV into []Record — one per row.
//     Quirks handled: Excel formula ISBN wrapping (="..."), YYYY/MM/DD
//     dates, titles of the form "Title (Series, #N)", comma-split
//     Additional Authors, 1 MiB per-field cap against pathological
//     exports.
//  2. Build a Resolver that caches a normalized lookup index of the
//     existing vault notes: by ISBN13 / ISBN10 / (normalized title +
//     normalized surname). Fuzzy title+author matches use the
//     Levenshtein-based Ratio from internal/strmatch.
//  3. BuildPlan pairs Records against the Resolver and produces a Plan
//     with will_create / will_update / will_skip / conflicts entries.
//     Per-field precedence follows SKILL.md §Data precedence, with the
//     template-default exception for status=unread.
//  4. Apply executes a previously-built Plan after the caller has taken
//     an explicit user confirmation. It first calls backup.Snapshot,
//     then writes each create/update atomically and drives the index
//     via sync.Apply.
//
// No network access. No telemetry. No side effects during parse or
// plan-build — the only file operations are the ones inside Apply.
package goodreads
