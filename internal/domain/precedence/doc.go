// Package precedence resolves which source wins for a given field value
// when multiple sources (vault, Goodreads, Audiobookshelf, Kavita, metadata
// provider) disagree or supply partial data. See SKILL.md §Data precedence
// for the authoritative ordering.
//
// External sources fill gaps; they never overwrite populated vault fields.
// A field is "populated" when its value is non-nil, non-empty (for strings
// and slices), and non-nil-pointer. An empty slice or empty string counts
// as a gap. The status field has a per-field override (see IsStatusGap)
// that also treats "unread" as a gap, because "unread" is the template
// default and not a deliberate user choice.
package precedence
