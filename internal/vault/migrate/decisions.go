package migrate

import "sort"

// Decision captures a user's action on a flagged Conflict. v0.2.1
// produces no conflicts semantically (all legacy scalars are
// unambiguous), but the shape mirrors goodreads.Decision and
// audiobookshelf.Decision so the HTTP surface can reuse the shared
// plan-render pipeline in app.js.
type Decision struct {
	Filename string
	Action   string // "accept" or "skip"
}

// ApplyDecisions is a no-op for v0.2.1: the Conflicts bucket has no
// structural alternative form to promote to (unlike the Goodreads /
// Audiobookshelf flows where "accept" means "trust the external value
// over the detected ambiguity"). For symmetry with those flows the
// function exists and threads through — if a future migration produces
// actionable conflicts (e.g., dual-shape notes where both a scalar and
// a map coexist), this is where the promotion logic lands.
func ApplyDecisions(plan *Plan, decisions []Decision) error {
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
	// No promotion logic today. Keeping the Conflicts bucket untouched
	// so a "skip" decision produces the same result as an absent one,
	// which the UI reports as "left in conflict" — correct for a
	// migration the user declined.
	_ = index
	sort.SliceStable(plan.Conflicts, func(i, j int) bool { return plan.Conflicts[i].Filename < plan.Conflicts[j].Filename })
	return nil
}
