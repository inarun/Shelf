package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
	"github.com/inarun/Shelf/internal/vault/paths"
)

// Plan lists the per-note outcomes of a rating-shape migration build.
// The three buckets — WillMigrate, WillSkip, Conflicts — mirror
// rename.Plan and audiobookshelf.Plan so the HTTP surface can reuse the
// shared render pipeline (makeSection / makeRadio / collectDecisions).
type Plan struct {
	WillMigrate []MigrateEntry  `json:"will_migrate"`
	WillSkip    []SkipEntry     `json:"will_skip"`
	Conflicts   []ConflictEntry `json:"conflicts"`
}

// MigrateEntry records one note whose rating is in the legacy scalar
// shape and can be rewritten to the canonical map. The staleness pair
// (planSize, planMtime) is captured at Plan time and re-checked in
// Apply to reject notes the user edited mid-flight.
type MigrateEntry struct {
	// Filename is the vault-relative .md filename (no path).
	Filename string `json:"filename"`
	// OldValue is the legacy scalar as parsed from disk.
	OldValue float64 `json:"old_value"`
	// Reason is a short human-readable explanation for the UI.
	Reason string `json:"reason"`
	// planSize / planMtime form the staleness pair; kept internal so the
	// JSON wire stays compact.
	planSize  int64
	planMtime int64
}

// PlanSize exposes the staleness pair size for tests that need to
// simulate between-plan-and-apply drift.
func (e MigrateEntry) PlanSize() int64 { return e.planSize }

// PlanMtime exposes the staleness pair mtime for tests.
func (e MigrateEntry) PlanMtime() int64 { return e.planMtime }

// SkipEntry is a note that BuildPlan elected not to touch — either
// because it already carries the map shape, or because it has no
// rating at all.
type SkipEntry struct {
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
}

// ConflictEntry records a note BuildPlan cannot auto-migrate. The
// handler surfaces these with accept/skip radio buttons mirroring the
// sync/import flows, though v0.2.1 produces no conflicts in practice
// (all legacy scalars are unambiguous).
type ConflictEntry struct {
	Filename          string `json:"filename"`
	Reason            string `json:"reason"`
	NeedsUserDecision bool   `json:"needs_user_decision"`
}

// BuildPlan scans every book row in the index, reads each note, and
// classifies it by frontmatter.RatingShape. Legacy scalars land in
// WillMigrate; map-shape or absent ratings land in WillSkip; read
// errors and parse failures become Conflicts.
//
// Walking the store (rather than the filesystem directly) matches
// rename.BuildPlan's shape and ensures only indexed notes are
// considered — Shelf can't migrate a file it doesn't know about.
func BuildPlan(ctx context.Context, s *store.Store, booksAbs string) (*Plan, error) {
	rows, err := s.ListBooks(ctx, store.Filter{})
	if err != nil {
		return nil, fmt.Errorf("migrate: list books: %w", err)
	}
	p := &Plan{}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fullPath, err := paths.ValidateWithinRoot(booksAbs, row.Filename)
		if err != nil {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Filename:          row.Filename,
				Reason:             fmt.Sprintf("path validation: %v", err),
				NeedsUserDecision: true,
			})
			continue
		}
		n, err := note.Read(fullPath)
		if err != nil {
			p.Conflicts = append(p.Conflicts, ConflictEntry{
				Filename:          row.Filename,
				Reason:             fmt.Sprintf("read note: %v", err),
				NeedsUserDecision: true,
			})
			continue
		}
		switch n.Frontmatter.RatingShape() {
		case frontmatter.RatingShapeAbsent:
			p.WillSkip = append(p.WillSkip, SkipEntry{
				Filename: row.Filename,
				Reason:   "no rating",
			})
		case frontmatter.RatingShapeMap:
			p.WillSkip = append(p.WillSkip, SkipEntry{
				Filename: row.Filename,
				Reason:   "already map-shape",
			})
		case frontmatter.RatingShapeLegacyScalar:
			r := n.Frontmatter.Rating()
			if r == nil || r.Overall == nil {
				// Shouldn't happen — the shape peek said scalar but the
				// semantic parse failed. Surface as a conflict.
				p.Conflicts = append(p.Conflicts, ConflictEntry{
					Filename:          row.Filename,
					Reason:             "legacy scalar failed to parse as a number",
					NeedsUserDecision: true,
				})
				continue
			}
			p.WillMigrate = append(p.WillMigrate, MigrateEntry{
				Filename:  row.Filename,
				OldValue:  *r.Overall,
				Reason:    "legacy scalar; rewrites as {overall: N, trial_system: {}}",
				planSize:  n.Size,
				planMtime: n.MtimeNanos,
			})
		default:
			p.WillSkip = append(p.WillSkip, SkipEntry{
				Filename: row.Filename,
				Reason:   "unknown rating shape",
			})
		}
	}
	// Deterministic order for testability + stable UI rendering.
	sort.Slice(p.WillMigrate, func(i, j int) bool { return p.WillMigrate[i].Filename < p.WillMigrate[j].Filename })
	sort.Slice(p.WillSkip, func(i, j int) bool { return p.WillSkip[i].Filename < p.WillSkip[j].Filename })
	sort.Slice(p.Conflicts, func(i, j int) bool { return p.Conflicts[i].Filename < p.Conflicts[j].Filename })
	return p, nil
}

// MarshalJSON ensures empty slices serialize as "[]" not "null" so the
// JS renderer can iterate without null guards.
func (p *Plan) MarshalJSON() ([]byte, error) {
	type jsonPlan struct {
		WillMigrate []MigrateEntry  `json:"will_migrate"`
		WillSkip    []SkipEntry     `json:"will_skip"`
		Conflicts   []ConflictEntry `json:"conflicts"`
	}
	out := jsonPlan{
		WillMigrate: p.WillMigrate,
		WillSkip:    p.WillSkip,
		Conflicts:   p.Conflicts,
	}
	if out.WillMigrate == nil {
		out.WillMigrate = []MigrateEntry{}
	}
	if out.WillSkip == nil {
		out.WillSkip = []SkipEntry{}
	}
	if out.Conflicts == nil {
		out.Conflicts = []ConflictEntry{}
	}
	return json.Marshal(out)
}

