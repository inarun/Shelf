package migrate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/atomic"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
	"github.com/inarun/Shelf/internal/vault/note"
)

// setupEnv spins up a temp vault, backups root, store, and syncer —
// the common scaffolding for every migrate test.
func setupEnv(t *testing.T) (booksAbs, backupsRoot string, s *store.Store, syncer *sync.Syncer) {
	t.Helper()
	dir := t.TempDir()
	booksAbs = filepath.Join(dir, "Books")
	backupsRoot = filepath.Join(dir, "backups")
	if err := os.MkdirAll(booksAbs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(backupsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(dir, "index.db")
	st, err := store.Open(sp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return booksAbs, backupsRoot, st, sync.New(st, booksAbs)
}

// seedFile writes a note with the exact YAML content provided; letting
// tests control the on-disk shape (legacy scalar vs. map) precisely.
func seedFile(t *testing.T, booksAbs, filename, content string) {
	t.Helper()
	if err := atomic.Write(filepath.Join(booksAbs, filename), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const legacyScalarNote = `---
title: Hyperion
authors:
  - Dan Simmons
status: finished
rating: 4
---
# Hyperion
`

const mapShapeNote = `---
title: Dune
authors:
  - Frank Herbert
status: finished
rating:
  trial_system:
    plot: 5
  overall: 5
---
# Dune
`

const noRatingNote = `---
title: Foo
authors:
  - Bar Baz
status: unread
---
# Foo
`

func TestBuildPlan_LegacyScalarMigrates(t *testing.T) {
	booksAbs, _, s, syncer := setupEnv(t)
	seedFile(t, booksAbs, "Hyperion by Dan Simmons.md", legacyScalarNote)
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(context.Background(), s, booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillMigrate) != 1 {
		t.Fatalf("WillMigrate len = %d, want 1; plan=%+v", len(p.WillMigrate), p)
	}
	if p.WillMigrate[0].Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("Filename got %q", p.WillMigrate[0].Filename)
	}
	if p.WillMigrate[0].OldValue != 4 {
		t.Errorf("OldValue got %v, want 4", p.WillMigrate[0].OldValue)
	}
	if p.WillMigrate[0].PlanSize() == 0 || p.WillMigrate[0].PlanMtime() == 0 {
		t.Errorf("staleness pair not populated: size=%d mtime=%d", p.WillMigrate[0].PlanSize(), p.WillMigrate[0].PlanMtime())
	}
}

func TestBuildPlan_MapShapeSkipped(t *testing.T) {
	booksAbs, _, s, syncer := setupEnv(t)
	seedFile(t, booksAbs, "Dune by Frank Herbert.md", mapShapeNote)
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(context.Background(), s, booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillMigrate) != 0 {
		t.Errorf("WillMigrate should be empty for map-shape; got %+v", p.WillMigrate)
	}
	if len(p.WillSkip) != 1 || !strings.Contains(p.WillSkip[0].Reason, "map-shape") {
		t.Errorf("Expected map-shape WillSkip entry; got %+v", p.WillSkip)
	}
}

func TestBuildPlan_AbsentRatingSkipped(t *testing.T) {
	booksAbs, _, s, syncer := setupEnv(t)
	seedFile(t, booksAbs, "Foo by Bar Baz.md", noRatingNote)
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(context.Background(), s, booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillMigrate) != 0 {
		t.Errorf("WillMigrate should be empty for no-rating; got %+v", p.WillMigrate)
	}
	if len(p.WillSkip) != 1 || !strings.Contains(p.WillSkip[0].Reason, "no rating") {
		t.Errorf("Expected no-rating WillSkip entry; got %+v", p.WillSkip)
	}
}

func TestBuildPlan_SortsDeterministically(t *testing.T) {
	booksAbs, _, s, syncer := setupEnv(t)
	seedFile(t, booksAbs, "Zebra by Author.md", legacyScalarNote)
	seedFile(t, booksAbs, "Apple by Author.md", legacyScalarNote)
	seedFile(t, booksAbs, "Middle by Author.md", legacyScalarNote)
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, _ := BuildPlan(context.Background(), s, booksAbs)
	if len(p.WillMigrate) != 3 {
		t.Fatalf("WillMigrate len = %d, want 3", len(p.WillMigrate))
	}
	want := []string{"Apple by Author.md", "Middle by Author.md", "Zebra by Author.md"}
	for i, e := range p.WillMigrate {
		if e.Filename != want[i] {
			t.Errorf("[%d] %q, want %q", i, e.Filename, want[i])
		}
	}
}

func TestPlanMarshalJSON_EmitsEmptyArrays(t *testing.T) {
	p := &Plan{}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"will_migrate":[]`) {
		t.Errorf("will_migrate not [] in %s", s)
	}
	if !strings.Contains(s, `"will_skip":[]`) {
		t.Errorf("will_skip not [] in %s", s)
	}
	if !strings.Contains(s, `"conflicts":[]`) {
		t.Errorf("conflicts not [] in %s", s)
	}
}

func TestApply_RewritesLegacyScalarAsMap(t *testing.T) {
	booksAbs, backupsRoot, s, syncer := setupEnv(t)
	path := filepath.Join(booksAbs, "Hyperion by Dan Simmons.md")
	seedFile(t, booksAbs, "Hyperion by Dan Simmons.md", legacyScalarNote)
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(context.Background(), s, booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Apply(context.Background(), p, booksAbs, ApplyOptions{Syncer: syncer, BackupsRoot: backupsRoot})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("unexpected per-entry errors: %+v", rep.Errors)
	}
	if len(rep.Migrated) != 1 || rep.Migrated[0] != "Hyperion by Dan Simmons.md" {
		t.Errorf("Migrated got %v", rep.Migrated)
	}
	if rep.BackupRoot == "" {
		t.Errorf("BackupRoot empty; pre-apply snapshot should have populated it")
	}
	// Re-read the note; assert shape flipped.
	n, err := note.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if n.Frontmatter.RatingShape() != frontmatter.RatingShapeMap {
		t.Errorf("post-apply RatingShape = %v, want Map", n.Frontmatter.RatingShape())
	}
	r := n.Frontmatter.Rating()
	if r == nil || r.Overall == nil || *r.Overall != 4 {
		t.Errorf("post-apply Rating.Overall = %v, want 4", r)
	}
	// Body should carry the `## Rating — ★ 4/5` block now (dual-write).
	if raw := string(n.Body.Serialize()); !strings.Contains(raw, "## Rating") {
		t.Errorf("body missing ## Rating section after migrate; body:\n%s", raw)
	}
}

func TestApply_StalePlanRejected(t *testing.T) {
	booksAbs, backupsRoot, s, syncer := setupEnv(t)
	path := filepath.Join(booksAbs, "Hyperion by Dan Simmons.md")
	seedFile(t, booksAbs, "Hyperion by Dan Simmons.md", legacyScalarNote)
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(context.Background(), s, booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	// Mutate the file between Plan and Apply → staleness pair
	// mismatch → entry moves into Errors.
	if err := atomic.Write(path, []byte(legacyScalarNote+"\nextra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Apply(context.Background(), p, booksAbs, ApplyOptions{Syncer: syncer, BackupsRoot: backupsRoot})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Migrated) != 0 {
		t.Errorf("stale entry should not migrate; got Migrated=%v", rep.Migrated)
	}
	if len(rep.Errors) != 1 || rep.Errors[0].Phase != "stale" {
		t.Errorf("expected 1 stale error; got %+v", rep.Errors)
	}
}

func TestApply_NoOpOnMapShape(t *testing.T) {
	booksAbs, _, s, syncer := setupEnv(t)
	seedFile(t, booksAbs, "Dune by Frank Herbert.md", mapShapeNote)
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(context.Background(), s, booksAbs)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Apply(context.Background(), p, booksAbs, ApplyOptions{Syncer: syncer, SkipBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Migrated) != 0 {
		t.Errorf("Migrated should be empty; got %v", rep.Migrated)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0] != "Dune by Frank Herbert.md" {
		t.Errorf("Skipped got %v", rep.Skipped)
	}
}

func TestApplyDecisions_IsNoOp(t *testing.T) {
	// v0.2.1's migrate produces no conflicts semantically, so
	// ApplyDecisions is a parity shim that leaves the plan untouched.
	p := &Plan{
		Conflicts: []ConflictEntry{{Filename: "x.md", Reason: "unknown", NeedsUserDecision: true}},
	}
	if err := ApplyDecisions(p, []Decision{{Filename: "x.md", Action: "accept"}}); err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) != 1 {
		t.Errorf("Conflicts length changed unexpectedly: %+v", p.Conflicts)
	}
}

func TestApply_RequiresSyncer(t *testing.T) {
	_, err := Apply(context.Background(), &Plan{}, "/tmp", ApplyOptions{})
	if err == nil {
		t.Fatal("Apply with nil Syncer should error")
	}
}
