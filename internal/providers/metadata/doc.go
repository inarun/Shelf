// Package metadata defines the MetadataProvider abstraction used by the
// add-book flow and any future metadata enrichment. Concrete providers
// (Open Library today; Hardcover later) live in subpackages and must
// satisfy Provider.
//
// The interface is deliberately narrow: Shelf only calls out to a
// metadata source when the user triggers it from the UI (per SKILL.md
// §Core Invariants #8 — no phone-home). Implementations are expected
// to enforce their own timeouts, size caps, and host allowlists.
package metadata
