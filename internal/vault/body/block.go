package body

import "time"

// Kind enumerates the recognized block categories plus a catch-all for
// unrecognized ##-sections and pre-heading preamble. Zero value is
// KindUnknown, which is the correct default for any block whose heading
// didn't match one of the recognized labels.
type Kind int

const (
	// KindUnknown is an unrecognized ##-section or preamble content that
	// sits before any heading. Its Parsed field is always nil; Raw is
	// authoritative.
	KindUnknown Kind = iota
	// KindH1 covers the leading H1 region: the `# Title` line, any
	// optional Rating line (legacy, pre-v0.2.1), and any prose before
	// the first ##-section.
	KindH1
	KindRating   // "## Rating — ★ N/5" (v0.2.1+; managed, regenerated from frontmatter)
	KindKeyIdeas // "## Key Ideas / Takeaways"
	KindNotes    // "## Notes"
	KindQuotes   // "## Quotes & Highlights"
	KindActions  // "## Actions"
	KindRelated  // "## Related"
	KindTimeline // "## Reading Timeline"
)

// recognizedH2 maps a trimmed ##-heading to its Kind. Case-sensitive per
// SKILL.md §Body schema.
var recognizedH2 = map[string]Kind{
	"Key Ideas / Takeaways": KindKeyIdeas,
	"Notes":                 KindNotes,
	"Quotes & Highlights":   KindQuotes,
	"Actions":               KindActions,
	"Related":               KindRelated,
	"Reading Timeline":      KindTimeline,
}

// canonicalHeading returns the heading text (without the "## " prefix)
// for recognized kinds. Used when regenerating a dirty block.
func canonicalHeading(k Kind) string {
	for text, kind := range recognizedH2 {
		if kind == k {
			return text
		}
	}
	return ""
}

// canonicalOrder returns the sort order used by EnsureSection when
// inserting a recognized section. H1 is first, Rating (v0.2.1) second
// so the scored summary sits above user-authored content; unknowns stay
// wherever they were.
func canonicalOrder(k Kind) int {
	switch k {
	case KindH1:
		return 0
	case KindRating:
		return 1
	case KindKeyIdeas:
		return 2
	case KindNotes:
		return 3
	case KindQuotes:
		return 4
	case KindActions:
		return 5
	case KindRelated:
		return 6
	case KindTimeline:
		return 7
	default:
		return 99
	}
}

// Body is the top-level parse result. Blocks appear in document order.
type Body struct {
	Blocks []Block
}

// Block stores the verbatim source bytes plus a typed view of the same
// content. Mutations call setters that flip dirty to true and update
// Parsed without touching Raw; Serialize then regenerates the block's
// output from Parsed. Unmodified blocks serialize straight from Raw for
// a byte-equivalent round-trip.
type Block struct {
	Kind   Kind
	Raw    []byte
	Parsed any
	dirty  bool
}

// Dirty reports whether the block has been mutated since parse.
func (b Block) Dirty() bool { return b.dirty }

// H1Parsed holds the extracted title and optional rating number from the
// leading H1 region. Rating is nil when no "Rating — N/5" line is
// present; otherwise 1..5 per SKILL.md §Frontmatter schema.
type H1Parsed struct {
	Title  string
	Rating *int
}

// ListParsed holds line-oriented bullet content for ##-sections like
// Key Ideas, Actions, and Related. Items[i] is the full item bytes
// including the "- " prefix but excluding the trailing newline.
type ListParsed struct {
	Items [][]byte
}

// TextParsed holds opaque text content for prose ##-sections like Notes
// and Quotes & Highlights. Text is the section body after the heading,
// with a single leading blank line trimmed.
type TextParsed struct {
	Text []byte
}

// TimelineParsed holds the parsed Reading Timeline events. Entries whose
// line matches "- YYYY-MM-DD — <text>" have a non-zero Date; entries
// that don't match are still present in Events with zero Date and the
// original line bytes preserved in Raw.
type TimelineParsed struct {
	Events []TimelineEvent
}

// TimelineEvent is a single line in the Reading Timeline section.
type TimelineEvent struct {
	Date time.Time
	Text string
	Raw  []byte // full original line, trailing newline included
}

// RatingParsed holds the axis values extracted from a `## Rating`
// section. Values keys are the canonical snake_case RatingAxes
// identifiers (e.g. "emotional_impact"). OverrideOverall is stamped
// only when the block was (re)generated from a frontmatter.Rating with
// an explicit Overall; plain parse-from-disk leaves it nil.
type RatingParsed struct {
	Values          map[string]int
	OverrideOverall *float64
}
