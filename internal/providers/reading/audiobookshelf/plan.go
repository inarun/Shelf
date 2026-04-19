package audiobookshelf

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/inarun/Shelf/internal/domain/precedence"
	"github.com/inarun/Shelf/internal/domain/timeline"
	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// Plan is the outcome of BuildPlan applied to a set of LibraryItems.
// Safe to serialize as JSON and present to the user for confirmation;
// Apply consumes the exact same Plan struct.
type Plan struct {
	WillUpdate []UpdateEntry    `json:"will_update"`
	Conflicts  []ConflictEntry  `json:"conflicts"`
	WillSkip   []SkipEntry      `json:"will_skip"`
	Unmatched  []UnmatchedEntry `json:"unmatched"`
}

// UpdateEntry describes a gap-fill on an existing note. NewEntries are
// the timeline.Entry values that would be written; Apply re-checks the
// plan staleness pair against the live file before touching it.
type UpdateEntry struct {
	Filename   string           `json:"filename"`
	Reason     string           `json:"reason"`
	NewEntries []timeline.Entry `json:"new_entries"`

	// Internal: not serialized.
	planSize  int64
	planMtime int64
	item      LibraryItem
}

// SkipEntry describes a matched item that needs no vault update —
// either because its timeline already reflects the external state, or
// because Merge dropped every external entry as an overlap.
type SkipEntry struct {
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
}

// ConflictEntry describes a match that cannot be auto-classified:
// fuzzy ambiguity, author mismatch, or multiple vault candidates. The
// UI surfaces these for explicit user resolution via ApplyDecisions.
type ConflictEntry struct {
	Filename          string `json:"filename,omitempty"`
	Reason            string `json:"reason"`
	NeedsUserDecision bool   `json:"needs_user_decision"`

	// Internal: carries the AB data so an accept-decision can promote
	// this entry to WillUpdate without re-fetching.
	item     LibraryItem
	sessions []ListeningSession
}

// UnmatchedEntry lists an AB library item with no vault candidate.
// S13 does not auto-create notes for unmatched items — the UI (S14)
// will surface them with a "create note?" affordance.
type UnmatchedEntry struct {
	ABItemID      string `json:"ab_item_id"`
	DisplayTitle  string `json:"display_title"`
	DisplayAuthor string `json:"display_author"`
	Reason        string `json:"reason"`
}

// BuildPlan pairs each item with a matched vault note (via rv) and
// classifies each pairing into one of the four buckets. It performs
// vault reads on matched items (to stamp the staleness pair and compute
// the vault-side timeline) but no writes.
//
// booksAbs must be the resolved Books folder absolute path. now is the
// plan timestamp, reserved for future reason strings; unused in v0.2.
func BuildPlan(
	ctx context.Context,
	items []LibraryItem,
	sessions []ListeningSession,
	rv *Resolver,
	booksAbs string,
	now time.Time,
) (*Plan, error) {
	p := &Plan{}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		res, ok := rv.Match(item)
		if !ok {
			p.Unmatched = append(p.Unmatched, UnmatchedEntry{
				ABItemID:      item.ID,
				DisplayTitle:  item.Media.Metadata.Title,
				DisplayAuthor: item.Media.Metadata.AuthorName,
				Reason:        "no ISBN match and no fuzzy title+author match ≥ 0.80",
			})
			continue
		}

		if res.NeedsUserDecision {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Filename:          res.Filename,
				Reason:            res.Reason,
				NeedsUserDecision: true,
				item:              item,
				sessions:          sessions,
			})
			continue
		}

		entry, err := classifyMatched(res, item, sessions, booksAbs)
		if err != nil {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Filename:          res.Filename,
				Reason:            err.Error(),
				NeedsUserDecision: true,
				item:              item,
				sessions:          sessions,
			})
			continue
		}
		switch v := entry.(type) {
		case UpdateEntry:
			p.WillUpdate = append(p.WillUpdate, v)
		case SkipEntry:
			p.WillSkip = append(p.WillSkip, v)
		}
	}

	sortPlanBuckets(p)
	_ = now // reserved; BuildPlan stamps no timestamps in reasons for now
	return p, nil
}

// classifyMatched reads the matched note, builds vault + external
// timeline entries, merges them, and returns either an UpdateEntry
// (gap fills remain) or a SkipEntry (no gap fills).
func classifyMatched(
	res MatchResult,
	item LibraryItem,
	sessions []ListeningSession,
	booksAbs string,
) (any, error) {
	fullPath, err := paths.ValidateWithinRoot(booksAbs, res.Filename)
	if err != nil {
		return nil, fmt.Errorf("match validation failed: %w", err)
	}
	n, err := note.Read(fullPath)
	if err != nil {
		return nil, fmt.Errorf("reading matched note: %w", err)
	}

	vaultEntries := VaultEntriesFromFrontmatter(n.Frontmatter.Started(), n.Frontmatter.Finished())
	externalEntries := ItemToEntries(item, sessions)
	merged := timeline.Merge(vaultEntries, externalEntries)

	newEntries := make([]timeline.Entry, 0, len(externalEntries))
	for _, e := range merged {
		if e.Source == precedence.SourceAudiobookshelf {
			newEntries = append(newEntries, e)
		}
	}
	if len(newEntries) == 0 {
		return SkipEntry{
			Filename: res.Filename,
			Reason:   "vault timeline already covers external progress",
		}, nil
	}
	return UpdateEntry{
		Filename:   res.Filename,
		Reason:     res.Reason,
		NewEntries: newEntries,
		planSize:   n.Size,
		planMtime:  n.MtimeNanos,
		item:       item,
	}, nil
}

// sortPlanBuckets sorts each slice in p so JSON output is deterministic.
func sortPlanBuckets(p *Plan) {
	sort.SliceStable(p.WillUpdate, func(i, j int) bool {
		return p.WillUpdate[i].Filename < p.WillUpdate[j].Filename
	})
	sort.SliceStable(p.Conflicts, func(i, j int) bool {
		return p.Conflicts[i].Filename < p.Conflicts[j].Filename
	})
	sort.SliceStable(p.WillSkip, func(i, j int) bool {
		return p.WillSkip[i].Filename < p.WillSkip[j].Filename
	})
	sort.SliceStable(p.Unmatched, func(i, j int) bool {
		if p.Unmatched[i].DisplayTitle != p.Unmatched[j].DisplayTitle {
			return p.Unmatched[i].DisplayTitle < p.Unmatched[j].DisplayTitle
		}
		return p.Unmatched[i].ABItemID < p.Unmatched[j].ABItemID
	})
}
