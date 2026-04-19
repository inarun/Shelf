package timeline

import (
	"sort"

	"github.com/inarun/Shelf/internal/domain/precedence"
)

// Merge combines vault-origin and external-origin entries under the
// data-precedence rules in SKILL.md §Data precedence:
//
//  1. De-dup by ExternalID: when two entries share a non-empty
//     ExternalID, the higher-priority Source wins (vault always beats
//     external if both carry the same id, though in practice ExternalID
//     is empty for vault entries).
//  2. De-dup by (Source, Start.Date()): collapses same-source same-day
//     duplicates — typical when an external provider replays a session
//     into the same UTC day.
//  3. Overlap: if an external entry's [Start, End] overlaps any vault
//     entry's [Start, End], the vault entry wins (Core Invariant #5).
//     An external entry with a zero End does not overlap a vault entry
//     whose Start is after the external entry's Start — "ongoing" is
//     treated as "up to now," not "up to infinity."
//  4. Remainder: external entries that fill gaps are kept.
//
// The result is stable-sorted by Start ascending; ties break by Source
// priority descending, then ExternalID, so outputs are deterministic
// and testable.
//
// Merge is pure — it does not mutate its inputs.
func Merge(vault, external []Entry) []Entry {
	// Rule 1 + 2 run on each side independently first so the overlap
	// check in rule 3 compares de-duplicated ranges.
	vaultDedup := dedup(vault)
	externalDedup := dedup(external)

	// Rule 3: drop external entries that overlap any vault entry.
	kept := make([]Entry, 0, len(externalDedup))
	for _, ext := range externalDedup {
		if overlapsAny(ext, vaultDedup) {
			continue
		}
		kept = append(kept, ext)
	}

	// Rule 1 across sources: if a vault entry shares an ExternalID with
	// a kept external entry (rare, but possible once a sync round-trips
	// through the vault), the vault copy wins.
	merged := make([]Entry, 0, len(vaultDedup)+len(kept))
	merged = append(merged, vaultDedup...)
	for _, ext := range kept {
		if ext.ExternalID != "" && hasExternalID(merged, ext.ExternalID) {
			continue
		}
		merged = append(merged, ext)
	}

	sort.SliceStable(merged, func(i, j int) bool {
		if !merged[i].Start.Equal(merged[j].Start) {
			return merged[i].Start.Before(merged[j].Start)
		}
		pi, pj := precedence.Priority(merged[i].Source), precedence.Priority(merged[j].Source)
		if pi != pj {
			return pi > pj
		}
		return merged[i].ExternalID < merged[j].ExternalID
	})
	return merged
}

// dedup applies rules 1 and 2 to a single-origin slice. Within a slice,
// rule 1's higher-priority tiebreak is redundant (entries share origin)
// so later duplicates simply overwrite earlier ones on ExternalID; for
// (Source, Date) the first occurrence wins so the earliest-sorted entry
// stays.
func dedup(in []Entry) []Entry {
	if len(in) == 0 {
		return nil
	}
	byID := map[string]int{}
	byDay := map[sourceDay]int{}
	out := make([]Entry, 0, len(in))
	for _, e := range in {
		if e.ExternalID != "" {
			if idx, ok := byID[e.ExternalID]; ok {
				// Keep the higher-priority entry (for cross-origin cases;
				// within a single slice priorities are equal and this is a
				// stable no-op).
				if precedence.Priority(e.Source) > precedence.Priority(out[idx].Source) {
					out[idx] = e
				}
				continue
			}
			byID[e.ExternalID] = len(out)
			out = append(out, e)
			continue
		}
		key := sourceDay{Source: e.Source, Day: e.Start.UTC().Truncate(24 * 3600 * 1e9).Unix()}
		if _, ok := byDay[key]; ok {
			continue
		}
		byDay[key] = len(out)
		out = append(out, e)
	}
	return out
}

// sourceDay is the composite key for rule 2's de-dup map.
type sourceDay struct {
	Source precedence.Source
	Day    int64 // unix seconds of truncated-to-UTC-day Start
}

// hasExternalID reports whether any entry in es carries the given id.
// Empty ids never match.
func hasExternalID(es []Entry, id string) bool {
	if id == "" {
		return false
	}
	for _, e := range es {
		if e.ExternalID == id {
			return true
		}
	}
	return false
}

// overlapsAny reports whether ext's [Start, End] overlaps any entry's
// range in vs. See rule 3 in Merge's doc. End == zero is treated as
// "up to ext.Start" — a point-in-time event — so it only overlaps vault
// ranges that contain ext.Start.
func overlapsAny(ext Entry, vs []Entry) bool {
	for _, v := range vs {
		if overlaps(ext, v) {
			return true
		}
	}
	return false
}

// overlaps implements the inclusive-range overlap test with a zero-End
// carve-out (see overlapsAny).
func overlaps(a, b Entry) bool {
	aStart, aEnd := a.Start, a.End
	bStart, bEnd := b.Start, b.End
	if aEnd.IsZero() {
		aEnd = aStart
	}
	if bEnd.IsZero() {
		bEnd = bStart
	}
	if aStart.IsZero() || bStart.IsZero() {
		return false
	}
	return !aEnd.Before(bStart) && !bEnd.Before(aStart)
}
