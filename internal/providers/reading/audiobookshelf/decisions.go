package audiobookshelf

import (
	"fmt"
	"sort"
)

// Decision captures a user's action on a single flagged conflict. The
// handler's ConflictDecision wire type maps one-for-one onto this.
// Mirrors goodreads.Decision for API symmetry.
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
// classifyMatched, and promoted to WillUpdate — or moved to WillSkip if
// Merge shows no gaps. Skipped decisions and conflicts without a
// decision remain in Conflicts.
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
		// action == "accept" — rebuild classification from the carried
		// item + sessions as if the match had been non-ambiguous.
		res := MatchResult{Filename: c.Filename, Reason: "user-accepted borderline match"}
		entry, err := classifyMatched(res, c.item, c.sessions, booksAbs)
		if err != nil {
			return fmt.Errorf("audiobookshelf: accept decision for %q: %w", c.Filename, err)
		}
		switch v := entry.(type) {
		case UpdateEntry:
			plan.WillUpdate = append(plan.WillUpdate, v)
		case SkipEntry:
			plan.WillSkip = append(plan.WillSkip, v)
		}
	}
	plan.Conflicts = remaining

	sort.SliceStable(plan.WillUpdate, func(i, j int) bool { return plan.WillUpdate[i].Filename < plan.WillUpdate[j].Filename })
	sort.SliceStable(plan.WillSkip, func(i, j int) bool { return plan.WillSkip[i].Filename < plan.WillSkip[j].Filename })
	return nil
}
