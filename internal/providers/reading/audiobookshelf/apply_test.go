package audiobookshelf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/vault/note"
)

func TestApply_UpdateGapFill(t *testing.T) {
	booksAbs, backupsRoot, s, sy := newVaultEnv(t)
	seedHyperion(t, booksAbs, sy, true)
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{abItem("ab-1", "Hyperion", "Dan Simmons", "9780553283686", true)}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-1", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_100_000_000},
	}
	p, err := BuildPlan(context.Background(), items, sessions, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer:      sy,
		BackupsRoot: backupsRoot,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(report.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %+v", report)
	}
	if report.BackupRoot == "" {
		t.Error("expected backup root populated on non-SkipBackup apply")
	}
	if _, err := os.Stat(report.BackupRoot); err != nil {
		t.Errorf("backup dir should exist: %v", err)
	}

	// Re-read the note and confirm frontmatter + body changed.
	gotBytes, err := os.ReadFile(filepath.Join(booksAbs, "Hyperion by Dan Simmons.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotBytes)
	if !strings.Contains(got, "status: finished") {
		t.Errorf("status not flipped to finished; got:\n%s", got)
	}
	if !strings.Contains(got, "## Reading Timeline") {
		t.Errorf("timeline section missing; got:\n%s", got)
	}
	if !strings.Contains(got, "Finished listening (Audiobookshelf)") {
		t.Errorf("expected finished-listening body line; got:\n%s", got)
	}

	// Index reflects the new status.
	row, err := s.GetBookByFilename(context.Background(), "Hyperion by Dan Simmons.md")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "finished" {
		t.Errorf("indexed status = %q", row.Status)
	}
	if len(row.FinishedDates) != 1 {
		t.Errorf("finished[] should have 1 entry, got %v", row.FinishedDates)
	}
}

func TestApply_StalePlanRejected(t *testing.T) {
	booksAbs, backupsRoot, s, sy := newVaultEnv(t)
	filename := seedHyperion(t, booksAbs, sy, true)
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{abItem("ab-1", "Hyperion", "Dan Simmons", "9780553283686", true)}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-1", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_100_000_000},
	}
	p, err := BuildPlan(context.Background(), items, sessions, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the file on disk to poison the staleness pair.
	fullPath := filepath.Join(booksAbs, filename)
	if err := os.WriteFile(fullPath, []byte("---\ntitle: Changed\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Sleep a hair to ensure mtime differs even on coarse-resolution NTFS.
	time.Sleep(20 * time.Millisecond)

	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer:      sy,
		BackupsRoot: backupsRoot,
		SkipBackup:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 staleness error, got %+v", report)
	}
	if !errors.Is(report.Errors[0].Err, note.ErrStale) {
		t.Errorf("expected ErrStale, got %v", report.Errors[0].Err)
	}
}

func TestApply_SkipsUpdatesThatMergeDropped(t *testing.T) {
	// Plan has a WillSkip entry — Apply should surface it in report.Skipped
	// with no writes or errors.
	booksAbs, backupsRoot, s, sy := newVaultEnv(t)

	// Pre-seed a fully-dated note so BuildPlan produces WillSkip.
	fmx := mustFinishedNote(t, booksAbs)
	_ = fmx
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{abItem("ab-1", "Hyperion", "Dan Simmons", "9780553283686", true)}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-1", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_100_000_000},
	}
	p, err := BuildPlan(context.Background(), items, sessions, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillSkip) != 1 {
		t.Fatalf("expected WillSkip, got %+v", p)
	}
	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer:      sy,
		BackupsRoot: backupsRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Updated) != 0 {
		t.Errorf("no updates expected, got %v", report.Updated)
	}
	if len(report.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %v", report.Skipped)
	}
}

func TestApplyDecisions_PromotesAcceptedConflict(t *testing.T) {
	// Build a plan where the item matches on title but authors differ,
	// landing in Conflicts. Accept the conflict and verify it moves to
	// WillUpdate.
	booksAbs, _, s, sy := newVaultEnv(t)
	mustUnreadNote(t, booksAbs, "The Expanse", "James Corey", "9780316129084")
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{abItem("ab-e", "The Expanse", "James Otherly", "", true)}
	sessions := []ListeningSession{
		{LibraryItemID: "ab-e", StartedAt: 1_700_000_000_000, UpdatedAt: 1_700_100_000_000},
	}
	p, err := BuildPlan(context.Background(), items, sessions, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", p)
	}

	err = ApplyDecisions(p, []Decision{{Filename: "The Expanse by James Corey.md", Action: "accept"}}, booksAbs)
	if err != nil {
		t.Fatalf("ApplyDecisions: %v", err)
	}
	if len(p.Conflicts) != 0 {
		t.Errorf("conflict should be drained, got %v", p.Conflicts)
	}
	if len(p.WillUpdate) != 1 {
		t.Errorf("conflict should be promoted to WillUpdate, got %v", p.WillUpdate)
	}
}

func TestApplyDecisions_SkipLeavesConflict(t *testing.T) {
	booksAbs, _, s, sy := newVaultEnv(t)
	mustUnreadNote(t, booksAbs, "The Expanse", "James Corey", "")
	if _, err := sy.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}
	rv, _ := NewResolver(context.Background(), s)

	items := []LibraryItem{abItem("ab-e", "The Expanse", "James Otherly", "", true)}
	p, err := BuildPlan(context.Background(), items, nil, rv, booksAbs, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	err = ApplyDecisions(p, []Decision{{Filename: "The Expanse by James Corey.md", Action: "skip"}}, booksAbs)
	if err != nil {
		t.Fatalf("ApplyDecisions: %v", err)
	}
	if len(p.Conflicts) != 1 {
		t.Errorf("skip should leave conflict in place, got %v", p.Conflicts)
	}
}

// mustFinishedNote writes a Hyperion note pre-dated with matching
// started/finished entries. Used by TestApply_SkipsUpdatesThatMergeDropped.
func mustFinishedNote(t *testing.T, booksAbs string) string {
	t.Helper()
	fm := newFM("Hyperion", "Dan Simmons")
	fm.SetISBN("9780553283686")
	fm.AppendStarted(time.UnixMilli(1_700_000_000_000).UTC())
	fm.AppendFinished(time.UnixMilli(1_700_100_000_000).UTC())
	_ = fm.SetStatus("finished")
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, nil)
	return "Hyperion by Dan Simmons.md"
}

func mustUnreadNote(t *testing.T, booksAbs, title, author, isbn string) {
	t.Helper()
	fm := newFM(title, author)
	if isbn != "" {
		fm.SetISBN(isbn)
	}
	_ = fm.SetStatus("unread")
	writeNote(t, booksAbs, title+" by "+author+".md", fm, nil)
}
