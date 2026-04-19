// Package timeline owns the semantic reading-timeline model: Entry (a
// single reading event) and Merge (the pure combiner that applies the
// §Data-precedence rules to vault and external-source entries).
//
// Entry is distinct from the lower-level types it neighbors:
//   - internal/vault/body.TimelineEvent is the raw free-text line parsed
//     from a note's "## Reading Timeline" section. It's a rendering
//     detail, not a semantic record.
//   - internal/http/handlers.TimelineEntry is a UI pair (started +
//     finished) composed for the book-detail page.
//
// Package timeline sits above both: it's what sync pipelines build and
// merge before any vault write happens. See merge.go for the de-dup +
// overlap rules.
package timeline
