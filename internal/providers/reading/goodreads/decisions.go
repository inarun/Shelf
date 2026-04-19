package goodreads

import (
	"fmt"
	"sort"

	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// Decision captures a user's action on a single flagged conflict. The
// handler's ConflictDecision wire type maps one-for-one onto this.
type Decision struct {
	// Filename identifies which plan.Conflicts entry this decision
	// refers to. Must exactly match ConflictEntry.Filename.
	Filename string
	// Action is "accept" or "skip". "accept" promotes the conflict to
	// WillUpdate (or WillSkip if no gap fills remain). "skip" leaves the
	// entry in Conflicts untouched.
	Action string
}

// ApplyDecisions mutates plan.Conflicts in-place per the decisions
// slice. Accepted entries are re-read from the vault, diffed through
// computeChanges, and promoted to WillUpdate — or moved to WillSkip if
// the vault is already fully populated relative to the CSV row. Skipped
// decisions and conflicts without a decision remain in Conflicts.
//
// Conflicts with an empty Filename (e.g., the "missing title/author"
// case) are not addressable by filename-keyed decisions and are always
// left in Conflicts.
//
// An error is returned only for per-entry failures that indicate the
// plan is inconsistent with the filesystem (e.g., the note was deleted
// between BuildPlan and ApplyDecisions). Returning the error short-
// circuits the entire operation so the caller can reject the Apply
// outright rather than produce a partial result.
func ApplyDecisions(plan *Plan, decisions []Decision, booksAbs string) error {
	if plan == nil || len(decisions) == 0 {
		return nil
	}
	index := make(map[string]string, len(decisions))
	for _, d := range decisions {
		if d.Filename == "" {
			continue
		}
		index[d.Filename] = d.Action
	}
	if len(index) == 0 {
		return nil
	}

	remaining := make([]ConflictEntry, 0, len(plan.Conflicts))
	for _, c := range plan.Conflicts {
		action, hasDecision := index[c.Filename]
		if c.Filename == "" || !hasDecision || action == "skip" {
			remaining = append(remaining, c)
			continue
		}
		// action == "accept"
		fullPath, err := paths.ValidateWithinRoot(booksAbs, c.Filename)
		if err != nil {
			return fmt.Errorf("goodreads: accept decision for %q: %w", c.Filename, err)
		}
		n, err := note.Read(fullPath)
		if err != nil {
			return fmt.Errorf("goodreads: accept decision for %q: read note: %w", c.Filename, err)
		}
		changes := computeChanges(c.record, n)
		if len(changes) == 0 {
			plan.WillSkip = append(plan.WillSkip, SkipEntry{
				Filename: c.Filename,
				Reason:   "user-accepted borderline match; no gaps to fill",
			})
			continue
		}
		plan.WillUpdate = append(plan.WillUpdate, UpdateEntry{
			Filename:  c.Filename,
			Reason:    "user-accepted borderline match",
			Changes:   changes,
			record:    c.record,
			planSize:  n.Size,
			planMtime: n.MtimeNanos,
		})
	}
	plan.Conflicts = remaining

	// Re-sort promoted entries so JSON output stays deterministic.
	sort.Slice(plan.WillUpdate, func(i, j int) bool { return plan.WillUpdate[i].Filename < plan.WillUpdate[j].Filename })
	sort.Slice(plan.WillSkip, func(i, j int) bool { return plan.WillSkip[i].Filename < plan.WillSkip[j].Filename })
	return nil
}
