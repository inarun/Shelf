package timeline

import (
	"time"

	"github.com/inarun/Shelf/internal/domain/precedence"
)

// Kind values for Entry.Kind. Not an enum type — leaves the field
// extensible (future sources may emit "paused", "dnf", etc. without a
// schema bump).
const (
	KindStarted  = "started"
	KindFinished = "finished"
	KindProgress = "progress"
)

// Entry is a single reading-timeline event and the semantic unit for
// sync merges. It is distinct from:
//   - body.TimelineEvent — a free-text line in a note's "## Reading Timeline"
//     section; rendered, not compared.
//   - templates.TimelineEntry — a paired started+finished UI row.
//
// Entry is what Merge operates on.
type Entry struct {
	// ExternalID is the foreign system's stable identifier for the event
	// (e.g., an Audiobookshelf session or library-item id). Empty for
	// vault-origin entries. Non-empty ExternalIDs are the primary de-dup
	// key across sources.
	ExternalID string

	// Source identifies the origin ranked per §Data precedence.
	Source precedence.Source

	// Start is the event's inclusive start time. Zero means unknown —
	// unusual for sync-derived entries but allowed for vault-origin
	// entries that predate timestamp stamping.
	Start time.Time

	// End is the event's inclusive end time. Zero means ongoing (e.g.,
	// a "Reading" entry that hasn't finished yet). A zero End does not
	// count as overlapping future vault entries during Merge.
	End time.Time

	// Kind is one of KindStarted / KindFinished / KindProgress. Callers
	// may emit other values; Merge doesn't branch on Kind today.
	Kind string

	// Note is optional free-text for rendering into a body "## Reading
	// Timeline" line. Merge passes it through unchanged.
	Note string
}
