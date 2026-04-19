package goodreads

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inarun/Shelf/internal/index/store"
	"github.com/inarun/Shelf/internal/index/sync"
	"github.com/inarun/Shelf/internal/vault/frontmatter"
)

func setupApplyEnv(t *testing.T) (booksAbs, backupsRoot string, s *store.Store, syncer *sync.Syncer) {
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
	s = openResolverStore(t)
	syncer = sync.New(s, booksAbs)
	return
}

func TestApply_CreateHappyPath(t *testing.T) {
	booksAbs, backupsRoot, s, syncer := setupApplyEnv(t)
	rv, _ := NewResolver(context.Background(), s)

	records := []Record{{
		RowNum:         1,
		Title:          "Hyperion",
		Author:         "Dan Simmons",
		Authors:        []string{"Dan Simmons"},
		ISBN13:         "9780553283686",
		MyRating:       5,
		ExclusiveShelf: "read",
		Status:         "finished",
		DateRead:       timeP(t, "2025-04-02"),
		Review:         "Loved the structure.\n## key takeaway about time",
	}}
	stamp := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	p, err := BuildPlan(context.Background(), records, rv, booksAbs, stamp)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer:      syncer,
		ImportStamp: stamp,
		BackupsRoot: backupsRoot,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created; got %v", report)
	}
	created := filepath.Join(booksAbs, "Hyperion by Dan Simmons.md")
	got, err := os.ReadFile(created)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "title: Hyperion") {
		t.Errorf("missing title; got:\n%s", got)
	}
	// Provenance line + blockquote wrap must appear.
	if !strings.Contains(string(got), "_Imported from Goodreads on 2026-04-17_") {
		t.Errorf("missing provenance line; got:\n%s", got)
	}
	// The "## key takeaway" line must have been blockquoted.
	if strings.Contains(string(got), "\n## key takeaway") {
		t.Errorf("review injection! found unquoted ## line; got:\n%s", got)
	}
	if !strings.Contains(string(got), "> ## key takeaway") {
		t.Errorf("expected blockquoted ## line; got:\n%s", got)
	}
	// Index reflects the new book.
	row, err := s.GetBookByFilename(context.Background(), "Hyperion by Dan Simmons.md")
	if err != nil {
		t.Fatalf("index lookup: %v", err)
	}
	if row.Title != "Hyperion" {
		t.Errorf("indexed title got %q", row.Title)
	}
}

func TestApply_UpdateGapFill(t *testing.T) {
	booksAbs, backupsRoot, s, syncer := setupApplyEnv(t)

	// Seed an existing, partially-populated note.
	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	_ = fm.SetStatus("unread")
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")
	// Index it — use FullScan to pick up the real (size, mtime).
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{
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
	}}
	stamp := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	p, err := BuildPlan(context.Background(), records, rv, booksAbs, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.WillUpdate) != 1 {
		t.Fatalf("expected update; got %+v", p)
	}

	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer: syncer, ImportStamp: stamp, BackupsRoot: backupsRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Updated) != 1 {
		t.Fatalf("expected 1 updated; got %v", report)
	}
	row, _ := s.GetBookByFilename(context.Background(), "Hyperion by Dan Simmons.md")
	if row.Status != "finished" {
		t.Errorf("status after apply got %q", row.Status)
	}
	if row.ISBN != "9780553283686" {
		t.Errorf("isbn after apply got %q", row.ISBN)
	}
	if row.RatingOverall == nil || *row.RatingOverall != 5.0 {
		t.Errorf("rating after apply got %v, want 5.0", row.RatingOverall)
	}
}

func TestApply_BackupCreated(t *testing.T) {
	booksAbs, backupsRoot, s, syncer := setupApplyEnv(t)
	// Pre-seed a file so there's something to back up.
	fm := frontmatter.NewEmpty()
	fm.SetTitle("X")
	fm.SetAuthors([]string{"A"})
	writeNote(t, booksAbs, "X by A.md", fm, "")

	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{
		RowNum: 1, Title: "Y", Author: "B", Authors: []string{"B"},
	}}
	p, _ := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())
	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer: syncer, BackupsRoot: backupsRoot, ImportStamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.BackupRoot == "" {
		t.Fatal("expected BackupRoot set")
	}
	if _, err := os.Stat(filepath.Join(report.BackupRoot, "X by A.md")); err != nil {
		t.Errorf("backup didn't capture seed file: %v", err)
	}
}

func TestApply_BackupFailureAborts(t *testing.T) {
	booksAbs, _, s, syncer := setupApplyEnv(t)
	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{
		RowNum: 1, Title: "Y", Author: "B", Authors: []string{"B"},
	}}
	p, _ := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())

	// BackupsRoot pointing at a non-existent directory makes backup.Snapshot
	// fail immediately; Apply must refuse to write.
	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer: syncer, BackupsRoot: "/definitely/does/not/exist/shelf",
		ImportStamp: time.Now(),
	})
	if err == nil {
		t.Fatal("expected backup error")
	}
	if report != nil {
		t.Errorf("report should be nil when backup fails, got %v", report)
	}
}

func TestApply_PlanStaleRefusedPerEntry(t *testing.T) {
	booksAbs, backupsRoot, s, syncer := setupApplyEnv(t)

	fm := frontmatter.NewEmpty()
	fm.SetTitle("Hyperion")
	fm.SetAuthors([]string{"Dan Simmons"})
	_ = fm.SetStatus("unread")
	writeNote(t, booksAbs, "Hyperion by Dan Simmons.md", fm, "")
	if _, err := syncer.FullScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	rv, _ := NewResolver(context.Background(), s)
	records := []Record{{
		RowNum: 1, Title: "Hyperion", Author: "Dan Simmons", Authors: []string{"Dan Simmons"},
		ExclusiveShelf: "read", Status: "finished", DateRead: timeP(t, "2025-04-02"),
	}}
	p, _ := BuildPlan(context.Background(), records, rv, booksAbs, time.Now())

	// Between plan and apply, modify the file externally.
	path := filepath.Join(booksAbs, "Hyperion by Dan Simmons.md")
	time.Sleep(10 * time.Millisecond) // ensure mtime differs on OSes with coarse granularity
	if err := os.WriteFile(path, []byte("---\ntitle: externally changed\n---\n# new body\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Apply(context.Background(), p, booksAbs, ApplyOptions{
		Syncer: syncer, BackupsRoot: backupsRoot, ImportStamp: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 error; got %v", report.Errors)
	}
	if report.Errors[0].Phase != "update" {
		t.Errorf("expected update-phase error; got %v", report.Errors[0])
	}
}

func TestPlanJSON_EmptySlicesRenderAsArrays(t *testing.T) {
	p := &Plan{}
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"will_create":[]`) {
		t.Errorf("expected [], got %s", s)
	}
	if !strings.Contains(s, `"will_update":[]`) {
		t.Errorf("expected [], got %s", s)
	}
	if !strings.Contains(s, `"will_skip":[]`) {
		t.Errorf("expected [], got %s", s)
	}
	if !strings.Contains(s, `"conflicts":[]`) {
		t.Errorf("expected [], got %s", s)
	}
}
