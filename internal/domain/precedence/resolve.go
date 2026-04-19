package precedence

import "reflect"

// Source identifies where a candidate value came from. Ordering (highest
// first): vault frontmatter > vault body > Goodreads > Audiobookshelf >
// Kavita > metadata. See SKILL.md §Data precedence.
type Source int

const (
	SourceUnknown Source = iota
	SourceVaultFrontmatter
	SourceVaultBody
	SourceGoodreads
	SourceAudiobookshelf
	SourceKavita
	SourceMetadata
)

// String returns a stable identifier useful for logging.
func (s Source) String() string {
	switch s {
	case SourceVaultFrontmatter:
		return "vault_frontmatter"
	case SourceVaultBody:
		return "vault_body"
	case SourceGoodreads:
		return "goodreads"
	case SourceAudiobookshelf:
		return "audiobookshelf"
	case SourceKavita:
		return "kavita"
	case SourceMetadata:
		return "metadata"
	default:
		return "unknown"
	}
}

// sourcePriority ranks sources so higher values win. Unknown ranks
// lowest; add new sources to the constants + this map.
var sourcePriority = map[Source]int{
	SourceVaultFrontmatter: 60,
	SourceVaultBody:        50,
	SourceGoodreads:        40,
	SourceAudiobookshelf:   30,
	SourceKavita:           20,
	SourceMetadata:         10,
	SourceUnknown:          0,
}

// Priority returns the ordering rank for s; higher wins.
func Priority(s Source) int {
	return sourcePriority[s]
}

// Candidate pairs a source with a value drawn from it.
type Candidate struct {
	Source Source
	Value  any
}

// Resolve picks the highest-priority populated candidate. Returns
// (zero, false) if every candidate is a gap per IsGap.
func Resolve(candidates []Candidate) (Candidate, bool) {
	return ResolveWith(candidates, IsGap)
}

// ResolveWith is Resolve with a caller-supplied gap predicate, for
// field-specific overrides (status treats "unread" as a gap).
func ResolveWith(candidates []Candidate, isGap func(any) bool) (Candidate, bool) {
	var (
		winner Candidate
		ok     bool
	)
	for _, c := range candidates {
		if isGap(c.Value) {
			continue
		}
		if !ok || Priority(c.Source) > Priority(winner.Source) {
			winner = c
			ok = true
		}
	}
	return winner, ok
}

// IsGap reports whether v counts as "not populated" for precedence
// purposes: nil, empty string, empty slice/map, or a nil pointer.
// Zero-valued numerics are treated as populated (callers with scalar-
// zero-as-gap semantics should wrap the value in a *T and use nil to
// signal absence, matching the frontmatter package's conventions).
func IsGap(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Ptr, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// IsStatusGap treats "" and "unread" as gaps per SKILL.md §Data
// precedence (status exception, dated 2026-04-16).
func IsStatusGap(v any) bool {
	if IsGap(v) {
		return true
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return s == "unread"
}
