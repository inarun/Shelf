package goodreads

import (
	"testing"

	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

// mkConflictPlan builds a Plan with one ConflictEntry whose record would,
// when fed through computeChanges against a vault note we prepare, produce
// a specific set of changes. Shares helpers with plan_test.go.
func mkConflictPlan(booksAbs, filename string, r Record) *Plan {
	return &Plan{
		Conflicts: []ConflictEntry{{
			Filename:          filename,
			Reason:            "borderline-match test fixture",
			NeedsUserDecision: true,
			CSVRow:            r.RowNum,
			record:            r,
		}},
	}
}

func TestApplyDecisions_AcceptPromotesToWillUpdate(t *testing.T) {
	booksAbs, _ := setupPlanEnv(t)

	// Vault has title+authors but nothing else; CSV fills many gaps.
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")

	rec := Record{
		RowNum:         1,
		Title:          "Hyperion",
		Author:         "Dan Simmons",
		Authors:        []string{"Dan Simmons"},
		ISBN13:         "9780553283686",
		Publisher:      "Bantam",
		YearPublished:  "1989",
		MyRating:       5,
		ExclusiveShelf: "read",
		Status:         "finished",
		DateRead:       timeP(t, "2025-04-02"),
	}
	plan := mkConflictPlan(booksAbs, "Hyperion by Dan Simmons.md", rec)
	decisions := []Decision{{Filename: "Hyperion by Dan Simmons.md", Action: "accept"}}

	if err := ApplyDecisions(plan, decisions, booksAbs); err != nil {
		t.Fatalf("ApplyDecisions: %v", err)
	}
	if len(plan.Conflicts) != 0 {
		t.Errorf("expected 0 conflicts after accept; got %+v", plan.Conflicts)
	}
	if len(plan.WillUpdate) != 1 {
		t.Fatalf("expected 1 WillUpdate; got %+v", plan.WillUpdate)
	}
	got := plan.WillUpdate[0]
	if got.Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("filename=%q", got.Filename)
	}
	if got.Reason != "user-accepted borderline match" {
		t.Errorf("reason=%q", got.Reason)
	}
	// Must have propagated the record for Apply to consume.
	if got.record.Title != "Hyperion" {
		t.Errorf("record lost between Conflict and WillUpdate: %+v", got.record)
	}
	if got.planSize == 0 || got.planMtime == 0 {
		t.Errorf("planSize/planMtime not set; drift detection will misfire: size=%d mtime=%d",
			got.planSize, got.planMtime)
	}
	fields := map[string]bool{}
	for _, c := range got.Changes {
		fields[c.Field] = true
	}
	for _, want := range []string{
		frontmatter.KeyISBN, frontmatter.KeyPublisher, frontmatter.KeyPublish,
		frontmatter.KeyRating, frontmatter.KeyStatus, frontmatter.KeyFinished,
		frontmatter.KeyReadCount,
	} {
		if !fields[want] {
			t.Errorf("expected change for %q; got fields %v", want, fields)
		}
	}
}

func TestApplyDecisions_AcceptWithNoChangesMovesToWillSkip(t *testing.T) {
	booksAbs, _ := setupPlanEnv(t)

	// Vault already fully populated relative to the CSV row.
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	fm.SetISBN("9780553283686")
	fm.SetPublisher("Bantam")
	fm.SetPublish("1989")
	_ = fm.SetStatus("finished")
	over := 5.0
	_ = fm.SetRating(&frontmatter.Rating{Overall: &over})
	fm.AppendFinished(*timeP(t, "2025-04-02"))
	fm.SetReadCount(1)
	fm.SetCategories([]string{"sci-fi"})
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")

	rec := Record{
		Title: "Hyperion", Author: "Dan Simmons", Authors: []string{"Dan Simmons"},
		ISBN13: "9780553283686", Publisher: "Bantam", YearPublished: "1989",
		MyRating: 5, ExclusiveShelf: "read", Status: "finished",
		DateRead:    timeP(t, "2025-04-02"),
		Bookshelves: []string{"sci-fi"},
	}
	plan := mkConflictPlan(booksAbs, "Hyperion by Dan Simmons.md", rec)
	decisions := []Decision{{Filename: "Hyperion by Dan Simmons.md", Action: "accept"}}

	if err := ApplyDecisions(plan, decisions, booksAbs); err != nil {
		t.Fatalf("ApplyDecisions: %v", err)
	}
	if len(plan.Conflicts) != 0 {
		t.Errorf("expected 0 conflicts after accept; got %+v", plan.Conflicts)
	}
	if len(plan.WillUpdate) != 0 {
		t.Errorf("expected 0 updates; got %+v", plan.WillUpdate)
	}
	if len(plan.WillSkip) != 1 {
		t.Fatalf("expected 1 WillSkip; got %+v", plan.WillSkip)
	}
	got := plan.WillSkip[0]
	if got.Filename != "Hyperion by Dan Simmons.md" {
		t.Errorf("filename=%q", got.Filename)
	}
	if got.Reason != "user-accepted borderline match; no gaps to fill" {
		t.Errorf("reason=%q", got.Reason)
	}
}

func TestApplyDecisions_SkipLeavesInConflicts(t *testing.T) {
	booksAbs, _ := setupPlanEnv(t)
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")

	rec := Record{
		Title: "Hyperion", Authors: []string{"Dan Simmons"},
		ISBN13: "9780553283686",
	}
	plan := mkConflictPlan(booksAbs, "Hyperion by Dan Simmons.md", rec)
	decisions := []Decision{{Filename: "Hyperion by Dan Simmons.md", Action: "skip"}}

	if err := ApplyDecisions(plan, decisions, booksAbs); err != nil {
		t.Fatalf("ApplyDecisions: %v", err)
	}
	if len(plan.Conflicts) != 1 {
		t.Errorf("expected 1 conflict after skip; got %+v", plan.Conflicts)
	}
	if len(plan.WillUpdate) != 0 || len(plan.WillSkip) != 0 {
		t.Errorf("skip must not promote; update=%+v skip=%+v", plan.WillUpdate, plan.WillSkip)
	}
}

func TestApplyDecisions_UnknownFilenameIgnored(t *testing.T) {
	booksAbs, _ := setupPlanEnv(t)
	plan := mkConflictPlan(booksAbs, "Real.md", Record{Title: "Real", Authors: []string{"A"}})
	// Decision for a different filename.
	decisions := []Decision{{Filename: "Other.md", Action: "accept"}}

	if err := ApplyDecisions(plan, decisions, booksAbs); err != nil {
		t.Fatalf("ApplyDecisions: %v", err)
	}
	if len(plan.Conflicts) != 1 {
		t.Errorf("unknown filename must not touch conflicts; got %+v", plan.Conflicts)
	}
}

func TestApplyDecisions_EmptyFilenameConflictLeftAlone(t *testing.T) {
	booksAbs, _ := setupPlanEnv(t)
	plan := &Plan{
		Conflicts: []ConflictEntry{{
			Filename:          "",
			Reason:            "missing title and/or author",
			NeedsUserDecision: true,
			CSVRow:            7,
		}},
	}
	// Decision carrying empty filename is dropped by indexer.
	decisions := []Decision{{Filename: "", Action: "accept"}}

	if err := ApplyDecisions(plan, decisions, booksAbs); err != nil {
		t.Fatalf("ApplyDecisions: %v", err)
	}
	if len(plan.Conflicts) != 1 {
		t.Errorf("empty-filename conflict must stay in Conflicts; got %+v", plan.Conflicts)
	}
}

func TestApplyDecisions_NilPlanIsNoop(t *testing.T) {
	if err := ApplyDecisions(nil, []Decision{{Filename: "x", Action: "accept"}}, ""); err != nil {
		t.Errorf("ApplyDecisions(nil, ...) = %v; want nil", err)
	}
}

func TestApplyDecisions_EmptyDecisionsIsNoop(t *testing.T) {
	plan := &Plan{Conflicts: []ConflictEntry{{Filename: "x.md"}}}
	if err := ApplyDecisions(plan, nil, ""); err != nil {
		t.Errorf("ApplyDecisions(..., nil) = %v; want nil", err)
	}
	if len(plan.Conflicts) != 1 {
		t.Errorf("empty decisions must leave plan alone; got %+v", plan.Conflicts)
	}
}

func TestApplyDecisions_MissingNoteReturnsError(t *testing.T) {
	booksAbs, _ := setupPlanEnv(t)
	// No note written — accepting the conflict must error because the
	// note is missing (drift between plan and apply).
	plan := mkConflictPlan(booksAbs, "Ghost.md", Record{Title: "Ghost", Authors: []string{"Nobody"}})
	decisions := []Decision{{Filename: "Ghost.md", Action: "accept"}}

	if err := ApplyDecisions(plan, decisions, booksAbs); err == nil {
		t.Fatal("ApplyDecisions: want non-nil error for missing note")
	}
}
