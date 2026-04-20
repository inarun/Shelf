// Package rules holds the rule-based recommender's six scorers and the
// combined ranker. Each scorer takes a candidate book + a Profile (built
// in internal/recommender/profile) and returns a Score with a normalized
// Value in [0, 1] and an optional user-facing Reason. Rank applies the
// Weights vector and surfaces the top-three weighted reasons per book.
//
// Filled in Session 18 (v0.3 mid). Session 19 consumes Rank's output to
// render the SSR /recommendations page; Session 17 already shipped the
// Profile and series.Detect inputs the scorers read.
package rules
